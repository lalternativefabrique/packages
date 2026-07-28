package edatest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"
)

// stubHandler is a configurable EventHandler used to drive the harness through
// both the compliant and the violating case.
type stubHandler struct {
	name        string
	subject     string
	durable     string
	maxDeliver  int
	concurrency int // 0 → does not implement ConcurrentHandler
	handle      func() error
	panicWith   any
}

func (s *stubHandler) Name() string        { return s.name }
func (s *stubHandler) Subject() string     { return s.subject }
func (s *stubHandler) DurableName() string { return s.durable }
func (s *stubHandler) MaxDeliver() int     { return s.maxDeliver }

func (s *stubHandler) Handle(context.Context, *nats.Msg) error {
	if s.panicWith != nil {
		panic(s.panicWith)
	}
	if s.handle != nil {
		return s.handle()
	}
	return nil
}

// concurrentStub adds the optional ConcurrentHandler interface.
type concurrentStub struct {
	*stubHandler
	concurrency int
}

func (c *concurrentStub) MaxConcurrency() int { return c.concurrency }

func validHandler(name string) *stubHandler {
	return &stubHandler{
		name:       name,
		subject:    "integration." + name,
		durable:    name,
		maxDeliver: 3,
	}
}

// The harness is only worth having if it FAILS on a violation, so every check
// is exercised against a deliberately broken handler. A contract test that
// cannot fail is decoration.
//
// t.Run with a nested testing.T would still mark the parent failed, so each
// case runs against a detached *testing.T whose result is inspected instead.
func TestVerifyHandlers_DetectsViolations(t *testing.T) {
	tests := []struct {
		name     string
		handlers []consumer.EventHandler
		wantFail bool
	}{
		{
			name:     "compliant handlers pass",
			handlers: []consumer.EventHandler{validHandler("a"), validHandler("b")},
		},
		{
			name: "duplicate durable name",
			handlers: []consumer.EventHandler{
				validHandler("a"),
				&stubHandler{name: "b", subject: "integration.b", durable: "a", maxDeliver: 3},
			},
			wantFail: true,
		},
		{
			name:     "empty durable name",
			handlers: []consumer.EventHandler{&stubHandler{name: "a", subject: "integration.a", maxDeliver: 3}},
			wantFail: true,
		},
		{
			name:     "empty subject",
			handlers: []consumer.EventHandler{&stubHandler{name: "a", durable: "a", maxDeliver: 3}},
			wantFail: true,
		},
		{
			name:     "empty name",
			handlers: []consumer.EventHandler{&stubHandler{subject: "integration.a", durable: "a", maxDeliver: 3}},
			wantFail: true,
		},
		{
			name:     "zero MaxDeliver never retries",
			handlers: []consumer.EventHandler{&stubHandler{name: "a", subject: "integration.a", durable: "a"}},
			wantFail: true,
		},
		{
			name:     "MaxDeliver above the ceiling",
			handlers: []consumer.EventHandler{&stubHandler{name: "a", subject: "integration.a", durable: "a", maxDeliver: 50}},
			wantFail: true,
		},
		{
			name: "ConcurrentHandler asking for zero concurrency",
			handlers: []consumer.EventHandler{
				&concurrentStub{stubHandler: validHandler("a"), concurrency: 0},
			},
			wantFail: true,
		},
		{
			name: "ConcurrentHandler with sane concurrency",
			handlers: []consumer.EventHandler{
				&concurrentStub{stubHandler: validHandler("a"), concurrency: 4},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failed := runDetached(func(dt *testing.T) {
				VerifyHandlers(dt, tc.handlers...)
			})
			if failed != tc.wantFail {
				t.Errorf("failed = %v, want %v", failed, tc.wantFail)
			}
		})
	}
}

// VerdictOf must mirror the engine's dispatch exactly; a divergence here would
// make every contract test assert on fiction.
func TestVerdictOf_MirrorsEngineDispatch(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Verdict
	}{
		{"nil is a success", nil, Ack},
		{"already done acks", consumer.AlreadyDone(errors.New("row exists")), Ack},
		{"permanent terminates", consumer.Permanent(errors.New("bad payload")), Permanent},
		{"anything else retries", errors.New("db down"), Retry},
		{"wrapped permanent still terminates", fmt.Errorf("ctx: %w", consumer.Permanent(errors.New("x"))), Permanent},
		{"wrapped already-done still acks", fmt.Errorf("ctx: %w", consumer.AlreadyDone(errors.New("x"))), Ack},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerdictOf(tc.err); got != tc.want {
				t.Errorf("VerdictOf(%v) = %s, want %s", tc.err, got, tc.want)
			}
		})
	}
}

// AlreadyDone is checked before Permanent, matching the engine. An error
// wrapping both means the effect IS in place, which is what the caller wanted
// whatever else the error says.
func TestVerdictOf_AlreadyDoneWinsOverPermanent(t *testing.T) {
	err := fmt.Errorf("%w: %w", consumer.ErrAlreadyDone, consumer.ErrPermanent)
	if got := VerdictOf(err); got != Ack {
		t.Errorf("VerdictOf = %s, want %s — the engine checks AlreadyDone first", got, Ack)
	}
}

func TestVerifyClassification_DetectsWrongVerdict(t *testing.T) {
	h := validHandler("a")
	h.handle = func() error { return errors.New("db down") } // retryable

	failed := runDetached(func(dt *testing.T) {
		VerifyClassification(dt, h, []Case{
			{Name: "misclassified", Data: []byte("{}"), Want: Permanent},
		})
	})
	if !failed {
		t.Error("a retryable error asserted as permanent should fail the contract check")
	}
}

func TestVerifyClassification_AcceptsCorrectVerdict(t *testing.T) {
	h := validHandler("a")
	h.handle = func() error { return consumer.Permanent(errors.New("malformed")) }

	failed := runDetached(func(dt *testing.T) {
		VerifyClassification(dt, h, []Case{
			{Name: "malformed payload", Data: []byte("{"), Want: Permanent},
		})
	})
	if failed {
		t.Error("a correctly classified permanent error should pass")
	}
}

// The check that matters most: a panic is not a failed message. The real
// consumer has no recover(), so it would unwind the goroutine and lose every
// in-flight message rather than this one.
func TestVerifyClassification_CatchesPanic(t *testing.T) {
	h := validHandler("a")
	h.panicWith = "nil dependency"

	failed := runDetached(func(dt *testing.T) {
		VerifyClassification(dt, h, []Case{
			{Name: "panics", Data: []byte("{}"), Want: Retry},
		})
	})
	if !failed {
		t.Error("a panicking handler must fail the contract check, not escape it")
	}
}

// runDetached runs fn as a real subtest of a throwaway parent and reports
// whether it failed, so a harness check can be asserted on without failing this
// suite.
//
// A bare &testing.T{} is not enough: t.Run on it does not record the subtest's
// result, so a violation reported inside VerifyClassification's own t.Run would
// look like a pass. testing.RunTests gives fn a T the framework actually drives.
func runDetached(fn func(*testing.T)) bool {
	passed := testing.RunTests(
		func(string, string) (bool, error) { return true, nil },
		[]testing.InternalTest{{
			Name: "detached",
			F:    fn,
		}},
	)
	return !passed
}
