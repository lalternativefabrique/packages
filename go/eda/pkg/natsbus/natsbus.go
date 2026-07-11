// Package natsbus provides a process-wide, brand-agnostic NATS + JetStream
// connection singleton. It carries no business logic and no product concept:
// any service can depend on it to obtain the shared bus without importing
// another service.
//
// The singleton connects lazily on first GetSharedConnection, keeps the
// connection alive across restarts of the broker (MaxReconnects(-1)), and
// exposes test hooks so integration/BDD suites that bring up their own NATS can
// install it as the shared connection.
package natsbus

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/logger"
)

// DefaultURL is used when NATS_URL is unset.
const DefaultURL = "nats://localhost:4222"

var (
	sharedConnection *nats.Conn
	sharedJS         nats.JetStreamContext
	connectionMu     sync.Mutex
	// log is the package logger for connection lifecycle events. Swap it with
	// SetLogger; defaults to a no-op so importing the package is silent.
	log logger.Logger = logger.Nop{}
)

// SetLogger installs the logger used for connection lifecycle events
// (disconnect / reconnect / closed). Call it once at startup before the first
// GetSharedConnection. Passing nil resets to a no-op logger.
func SetLogger(l logger.Logger) {
	connectionMu.Lock()
	defer connectionMu.Unlock()
	if l == nil {
		l = logger.Nop{}
	}
	log = l
}

// GetSharedConnection returns the singleton NATS + JetStream connection,
// establishing it on first call. The URL is read from NATS_URL (default
// DefaultURL). All callers in a process share one connection; the returned
// values must not be closed by callers — use CloseSharedConnection.
func GetSharedConnection() (*nats.Conn, nats.JetStreamContext, error) {
	connectionMu.Lock()
	defer connectionMu.Unlock()

	if sharedConnection != nil && sharedConnection.IsConnected() {
		return sharedConnection, sharedJS, nil
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = DefaultURL
	}

	nc, err := nats.Connect(
		natsURL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Warn("nats disconnected", logger.String("error", err.Error()))
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			log.Info("nats reconnected")
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.Info("nats connection closed")
		}),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to nats: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("get jetstream context: %w", err)
	}

	sharedConnection = nc
	sharedJS = js

	log.Info("nats shared connection established", logger.String("url", natsURL))
	return nc, js, nil
}

// CloseSharedConnection gracefully closes the shared connection and clears the
// singleton so a later GetSharedConnection reconnects.
func CloseSharedConnection() {
	connectionMu.Lock()
	defer connectionMu.Unlock()

	if sharedConnection != nil {
		sharedConnection.Close()
		sharedConnection = nil
		sharedJS = nil
		log.Info("nats shared connection closed")
	}
}

// SetSharedConnectionForTest installs an externally-owned connection as the
// singleton, for integration/BDD tests that bring up NATS themselves. The
// caller retains ownership and must not rely on CloseSharedConnection to close
// a connection it installed.
func SetSharedConnectionForTest(nc *nats.Conn, js nats.JetStreamContext) {
	connectionMu.Lock()
	defer connectionMu.Unlock()
	sharedConnection = nc
	sharedJS = js
}

// IsConnected reports whether the shared connection is active.
func IsConnected() bool {
	connectionMu.Lock()
	defer connectionMu.Unlock()
	return sharedConnection != nil && sharedConnection.IsConnected()
}
