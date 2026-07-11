package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestPermanentWrapsSentinel(t *testing.T) {
	base := errors.New("bad payload")
	err := Permanent(base)
	if !errors.Is(err, ErrPermanent) {
		t.Fatalf("Permanent() result should match ErrPermanent")
	}
	if !errors.Is(err, base) {
		t.Fatalf("Permanent() should preserve the wrapped error")
	}
	if Permanent(nil) != nil {
		t.Fatalf("Permanent(nil) should be nil")
	}
}

func TestTransientErrorIsNotPermanent(t *testing.T) {
	if errors.Is(errors.New("timeout"), ErrPermanent) {
		t.Fatalf("a plain error must not be treated as permanent")
	}
}

func TestExtractEventID(t *testing.T) {
	cases := map[string]struct {
		data []byte
		want string
	}{
		"present":   {[]byte(`{"event_id":"abc","x":1}`), "abc"},
		"absent":    {[]byte(`{"x":1}`), ""},
		"malformed": {[]byte(`not json`), ""},
		"empty":     {[]byte(``), ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := extractEventID(tc.data); got != tc.want {
				t.Fatalf("extractEventID(%q) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	var c Config
	c.withDefaults()

	if c.StreamName != "INTEGRATION_PIPELINE" {
		t.Errorf("StreamName default = %q", c.StreamName)
	}
	if c.DLQStreamName != "DLQ" {
		t.Errorf("DLQStreamName default = %q", c.DLQStreamName)
	}
	if c.AckWait != 30*time.Second {
		t.Errorf("AckWait default = %v", c.AckWait)
	}
	if len(c.BackOff) != 3 {
		t.Errorf("BackOff default len = %d, want 3", len(c.BackOff))
	}
	if c.MaxAckPending != 1000 {
		t.Errorf("MaxAckPending default = %d", c.MaxAckPending)
	}
	if c.Logger == nil {
		t.Error("Logger default should be non-nil (Nop)")
	}
}

// seqHandler is a minimal EventHandler with no concurrency opt-in.
type seqHandler struct{}

func (seqHandler) Name() string                            { return "seq" }
func (seqHandler) Subject() string                         { return "integration.>" }
func (seqHandler) DurableName() string                     { return "seq-consumer" }
func (seqHandler) MaxDeliver() int                         { return 3 }
func (seqHandler) Handle(context.Context, *nats.Msg) error { return nil }

// concHandler additionally implements ConcurrentHandler.
type concHandler struct{ seqHandler }

func (concHandler) MaxConcurrency() int { return 8 }

func TestHandlerConcurrency(t *testing.T) {
	if got := handlerConcurrency(seqHandler{}); got != 1 {
		t.Errorf("sequential handler concurrency = %d, want 1", got)
	}
	if got := handlerConcurrency(concHandler{}); got != 8 {
		t.Errorf("concurrent handler concurrency = %d, want 8", got)
	}
}
