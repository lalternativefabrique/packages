package webhooks

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalternative/packages/go/webhooks/applications/create_endpoint"
	"github.com/lalternative/packages/go/webhooks/applications/delete_endpoint"
	"github.com/lalternative/packages/go/webhooks/applications/get_endpoint"
	"github.com/lalternative/packages/go/webhooks/applications/list_endpoints"
	"github.com/lalternative/packages/go/webhooks/applications/rotate_secret"
	"github.com/lalternative/packages/go/webhooks/applications/update_endpoint"
	"github.com/lalternative/packages/go/webhooks/domain/events"
	"github.com/lalternative/packages/go/webhooks/infrastructure"
	"github.com/lalternative/packages/go/webhooks/infrastructure/dispatcher"
	"github.com/lalternative/packages/go/webhooks/infrastructure/outbox"
	"github.com/lalternative/packages/go/webhooks/infrastructure/projections"
	infrarepo "github.com/lalternative/packages/go/webhooks/infrastructure/repository"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/lalternative/packages/go/eda/pkg/consumer"
	"github.com/lalternative/packages/go/eda/pkg/pgprojector"
)

type Service struct {
	Create *create_endpoint.Handler
	List   *list_endpoints.Handler
	Get    *get_endpoint.Handler
	Update *update_endpoint.Handler
	Delete *delete_endpoint.Handler
	Rotate *rotate_secret.Handler

	createCtl *create_endpoint.Controller
	listCtl   *list_endpoints.Controller
	getCtl    *get_endpoint.Controller
	updateCtl *update_endpoint.Controller
	deleteCtl *delete_endpoint.Controller
	rotateCtl *rotate_secret.Controller

	pgProjector *pgprojector.Projector
	kvProjector *projections.EndpointsKVProjector
	dispatcher  *dispatcher.Dispatcher
	worker      *outbox.Worker

	nc *nats.Conn
}

type ServiceDeps struct {
	NC   *nats.Conn
	Pool *pgxpool.Pool
	// Brand names the outgoing HTTP headers (e.g. "Spore" yields
	// Spore-Signature). Empty falls back to outbox.DefaultBrand.
	Brand string
	// Catalog is the set of public event types subscribers may register for.
	// Required: an empty catalog rejects every endpoint, which is the honest
	// failure for a service that has declared no events.
	Catalog events.Catalog
	// Source is the upstream stream to fan out, and how its event types map
	// onto the public ones. The zero value disables dispatching: a product can
	// manage endpoints before it publishes anything.
	Source dispatcher.Source
}

func NewService(deps ServiceDeps) (*Service, error) {
	if deps.NC == nil {
		return nil, fmt.Errorf("nats connection is required")
	}
	js, err := deps.NC.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	// v2 jetstream context for pieces migrated onto the shared eda lib. During
	// the incremental migration both coexist: js (v1) for the not-yet-migrated
	// event store / projectors / worker, jsV2 for the eda-backed pieces.
	jsV2, err := jetstream.New(deps.NC)
	if err != nil {
		return nil, fmt.Errorf("jetstream v2: %w", err)
	}

	eventStore, err := infrastructure.NewNATSEventStore(context.Background(), deps.NC)
	if err != nil {
		return nil, fmt.Errorf("event store: %w", err)
	}

	kv, err := infrarepo.NewEndpointsKV(context.Background(), jsV2)
	if err != nil {
		return nil, fmt.Errorf("kv bucket: %w", err)
	}

	outboxPub, err := outbox.NewNATSOutboxPublisher(js)
	if err != nil {
		return nil, fmt.Errorf("outbox publisher: %w", err)
	}

	httpDispatcher := outbox.NewHTTPDispatcher(10*time.Second, deps.Brand)
	worker := outbox.NewWorker(eventStore, httpDispatcher)

	// Nil unless a Source is declared: endpoint management stands on its own,
	// and a product can expose it before it publishes a single event. Building
	// one anyway would leave StartBackground looping on a Run that can only
	// fail.
	var disp *dispatcher.Dispatcher
	if deps.Source.StreamName != "" {
		disp = dispatcher.NewDispatcher(js, kv, outboxPub, deps.Source)
	}

	svc := &Service{
		Create:      create_endpoint.NewHandler(eventStore, deps.Catalog),
		Update:      update_endpoint.NewHandler(eventStore, deps.Catalog),
		Delete:      delete_endpoint.NewHandler(eventStore),
		Rotate:      rotate_secret.NewHandler(eventStore),
		dispatcher:  disp,
		worker:      worker,
		kvProjector: projections.NewEndpointsKVProjector(kv),
		nc:          deps.NC,
	}
	svc.createCtl = create_endpoint.NewController(svc.Create)
	svc.updateCtl = update_endpoint.NewController(svc.Update)
	svc.deleteCtl = delete_endpoint.NewController(svc.Delete)
	svc.rotateCtl = rotate_secret.NewController(svc.Rotate)

	if deps.Pool != nil {
		reader := infrarepo.NewEndpointReaderPG(deps.Pool)
		svc.List = list_endpoints.NewHandler(reader)
		svc.Get = get_endpoint.NewHandler(reader)
		svc.listCtl = list_endpoints.NewController(svc.List)
		svc.getCtl = get_endpoint.NewController(svc.Get)
		svc.pgProjector = projections.NewEndpointsPGProjector(deps.Pool)
	}

	return svc, nil
}

// StartBackground starts the projectors, dispatcher and outbox worker.
func (s *Service) StartBackground(ctx context.Context) {
	if s.pgProjector != nil {
		go consumer.Run(ctx, s.nc, s.pgProjector, consumer.Config{
			StreamName:     infrastructure.StreamName,
			StreamSubjects: []string{infrastructure.AggregateSubjectFilter},
		})
	}
	if s.kvProjector != nil {
		go consumer.Run(ctx, s.nc, s.kvProjector, consumer.Config{
			StreamName:     infrastructure.StreamName,
			StreamSubjects: []string{infrastructure.AggregateSubjectFilter},
		})
	}
	if s.dispatcher != nil {
		go func() {
			if err := s.dispatcher.Run(ctx); err != nil {
				log.Printf("webhooks: dispatcher stopped: %v", err)
			}
		}()
	}
	if s.worker != nil {
		go consumer.Run(ctx, s.nc, s.worker, consumer.Config{
			StreamName:      outbox.StreamName,
			StreamSubjects:  []string{"outbox.webhook.>"},
			StreamRetention: jetstream.WorkQueuePolicy,
			BackOff:         outbox.BackOff(),
		})
	}
}
