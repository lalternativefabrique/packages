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

func TestAlreadyDoneWrapsSentinel(t *testing.T) {
	base := errors.New("unique violation")
	err := AlreadyDone(base)
	if !errors.Is(err, ErrAlreadyDone) {
		t.Fatalf("AlreadyDone() result should match ErrAlreadyDone")
	}
	if !errors.Is(err, base) {
		t.Fatalf("AlreadyDone() should preserve the wrapped error")
	}
	if AlreadyDone(nil) != nil {
		t.Fatalf("AlreadyDone(nil) should be nil")
	}
	// An already-done error must not be confused with a permanent one: they
	// drive different ack decisions (Ack vs Term).
	if errors.Is(err, ErrPermanent) {
		t.Fatalf("ErrAlreadyDone must not match ErrPermanent")
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
	if c.ClaimTTL != 3*time.Minute {
		t.Errorf("ClaimTTL default = %v, want 3m", c.ClaimTTL)
	}
	if c.Logger == nil {
		t.Error("Logger default should be non-nil (Nop)")
	}
}

// fakeIdempotency records the claim lifecycle so tests can assert the
// pre-claim ordering (claim before handler, done/release after).
type fakeIdempotency struct {
	claimOK  bool
	claimErr error
	calls    []string
}

func (f *fakeIdempotency) TryClaim(_ context.Context, _, eventID string, _ time.Duration) (bool, error) {
	f.calls = append(f.calls, "claim:"+eventID)
	return f.claimOK, f.claimErr
}
func (f *fakeIdempotency) MarkDone(_ context.Context, _, eventID string) error {
	f.calls = append(f.calls, "done:"+eventID)
	return nil
}
func (f *fakeIdempotency) Release(_ context.Context, _, eventID string) error {
	f.calls = append(f.calls, "release:"+eventID)
	return nil
}

var _ IdempotencyStore = (*fakeIdempotency)(nil)

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
