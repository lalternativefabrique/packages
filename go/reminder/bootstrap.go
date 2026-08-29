// Package reminder is the bounded context for coming back to someone later:
// storage, delivery-time polling, and dispatch to whichever channels a
// reminder names.
package reminder

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/reminder/application"
	"github.com/lalternative/packages/go/reminder/domain"
	"github.com/lalternative/packages/go/reminder/infrastructure"
)

// Service is the context's HTTP-facing facade.
type Service struct {
	reminders domain.Repository
	userIDFn  UserIDFunc
}

// ServiceDeps wires the context.
type ServiceDeps struct {
	Pool *pgxpool.Pool
	NC   *nats.Conn
	// UserID extracts the caller's id from a request. Required for the
	// reminder to be scoped per user; a nil func leaves every reminder
	// unscoped (UserID ""), visible to any caller.
	UserID UserIDFunc
	// Channels are the delivery channels available to reminders created
	// through this service (e.g. infrastructure.NewSlackChannel()). A
	// reminder whose Channels reference a type not listed here logs a
	// warning at fire time and is otherwise skipped for that channel.
	// Empty disables the dispatcher: reminders are still stored and fired,
	// but nothing outside the API/chat surface is contacted.
	Channels []domain.Channel
	// PollInterval overrides the default 15s delivery check. Zero keeps the
	// default.
	PollInterval time.Duration
}

// NewService wires the context, starts delivering what comes due, and — if
// Channels is non-empty — starts dispatching fired reminders to them.
func NewService(ctx context.Context, deps ServiceDeps) (*Service, error) {
	repo := infrastructure.NewPostgresRepository(deps.Pool)

	application.NewPoller(application.PollerConfig{
		Reminders: repo,
		Nats:      deps.NC,
		Interval:  deps.PollInterval,
	}).Start(ctx)

	if len(deps.Channels) > 0 && deps.NC != nil {
		disp := application.NewDispatcher(deps.Channels...)
		if err := disp.Start(ctx, deps.NC); err != nil {
			return nil, err
		}
	}

	return &Service{reminders: repo, userIDFn: deps.UserID}, nil
}

// RegisterRoutes mounts the context under g, e.g. g.Group("/reminders") or
// any group the host app already scopes to authenticated requests.
func (s *Service) RegisterRoutes(g *echo.Group) {
	g.POST("/reminders", s.CreateReminder)
	g.GET("/reminders", s.ListReminders)
	g.PATCH("/reminders/:id", s.UpdateReminder)
	g.DELETE("/reminders/:id", s.CancelReminder)
	g.POST("/reminders/:id/done", s.MarkReminderDone)
}

// Repository exposes the reminder store, so another bounded context (e.g. a
// chat agent's tools) can read/write reminders without a second connection to
// the same rows.
func (s *Service) Repository() domain.Repository { return s.reminders }
