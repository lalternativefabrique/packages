// Runnable example of a durable JetStream work-queue consumer.
//
// It shows the only thing application code should ever write — a handler that
// implements consumer.EventHandler and a single Handle method. Term-on-
// permanent, bounded MaxDeliver, staged BackOff, the DLQ stream, ack
// heartbeats and the reconnect loop are all provided by pkg/consumer.
//
// Requires a JetStream-enabled NATS server:
//
//	nats-server -js
//	go run ./examples/consumer
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"
	"github.com/lalternative/packages/go/eda/pkg/logger"
)

// contentCreated is the event payload this handler expects.
type contentCreated struct {
	EventID string `json:"event_id"`
	SrcID   string `json:"src_id"`
	URL     string `json:"url"`
}

// indexHandler is the entire application surface: it implements EventHandler
// and writes business logic in Handle. No JetStream wiring here.
type indexHandler struct{}

func (indexHandler) Name() string        { return "index" }
func (indexHandler) Subject() string     { return "integration.source.content.created" }
func (indexHandler) DurableName() string { return "index-content" }
func (indexHandler) MaxDeliver() int     { return 3 }

func (indexHandler) Handle(ctx context.Context, msg *nats.Msg) error {
	var ev contentCreated
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		// Malformed payload: no retry will ever fix it -> dead-letter it now.
		return consumer.Permanent(fmt.Errorf("decode content.created: %w", err))
	}
	if ev.URL == "" {
		return consumer.Permanent(errors.New("url is required"))
	}

	// ... real indexing work; a returned (non-permanent) error here is
	// retried with the staged BackOff up to MaxDeliver, then dead-lettered.
	fmt.Printf("indexed %s (%s)\n", ev.SrcID, ev.URL)
	return nil
}

func main() {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}
	nc, err := nats.Connect(natsURL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats connect: %v\n", err)
		os.Exit(1)
	}
	defer nc.Drain()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log := logger.NewSlogLogger(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	// Run blocks until ctx is cancelled, auto-reconnecting on drops.
	consumer.Run(ctx, nc, indexHandler{}, consumer.Config{Logger: log})
}
