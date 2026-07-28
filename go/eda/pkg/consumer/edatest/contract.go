// Package edatest verifies that application handlers honour the EventHandler
// contract.
//
// The consumer package's own tests prove the ENGINE keeps its side of the
// bargain: a permanent error terminates, a transient one is redelivered until
// the DLQ. Nothing proves the other side — that a handler returns the right
// kind of error in the first place, or that two handlers do not quietly share a
// durable name. That gap is where the expensive bugs live, because none of them
// surface as a failure:
//
//   - Two handlers on one durable name consume each other's messages. Each sees
//     roughly half the stream. No error is logged anywhere.
//   - A plain fmt.Errorf for a payload no retry can fix costs MaxDeliver
//     redeliveries before the DLQ takes a message that was doomed from the
//     first attempt.
//   - A panic is not a failed message: there is no recover() in the consumer
//     loop, so it unwinds the goroutine and takes every in-flight message with
//     it, up to MaxConcurrency.
//
// Usage — one test per application, listing the handlers it registers:
//
//	func TestHandlerContract(t *testing.T) {
//	    edatest.VerifyHandlers(t, myHandlers()...)
//	}
//
// And for the failure classification, which only the handler's own author can
// describe, per handler:
//
//	edatest.VerifyClassification(t, h, []edatest.Case{
//	    {Name: "malformed payload", Data: []byte("{"), Want: edatest.Permanent},
//	    {Name: "unknown type",      Data: mustJSON(evt), Want: edatest.Permanent},
//	})
package edatest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"
)

// maxDeliverCeiling is an upper bound on sane retry budgets. It is not a rule
// of the protocol — JetStream accepts any value — but a handler asking for
// dozens of attempts is nearly always classifying a permanent failure as
// transient, and pays for it with a message that fails identically for minutes.
const maxDeliverCeiling = 10

// VerifyHandlers asserts the contract every EventHandler must satisfy,
// including the cross-handler rules that no single handler can check about
// itself. Pass the complete set an application registers: a duplicate durable
// name is only visible when the whole fleet is inspected at once.
func VerifyHandlers(t *testing.T, handlers ...consumer.EventHandler) {
	t.Helper()

	if len(handlers) == 0 {
		t.Fatal("VerifyHandlers called with no handlers; pass the set the application registers")
	}

	for _, h := range handlers {
		verifyIdentity(t, h)
	}
	verifyDurableNamesUnique(t, handlers)
}

// verifyIdentity checks the fields a handler declares about itself. They are
// read once at subscription time, so a wrong value fails at startup or, worse,
// silently binds the consumer to a subject nobody publishes to — a handler that
// never runs looks exactly like one with nothing to do.
func verifyIdentity(t *testing.T, h consumer.EventHandler) {
	t.Helper()

	name := h.Name()
	if name == "" {
		t.Errorf("%T: Name() is empty; it identifies the handler in every log line and metric", h)
		name = fmt.Sprintf("%T", h)
	}
	if h.Subject() == "" {
		t.Errorf("%s: Subject() is empty; the consumer would bind to nothing", name)
	}
	if h.DurableName() == "" {
		t.Errorf("%s: DurableName() is empty; it is the JetStream cursor and the idempotency scope", name)
	}

	switch d := h.MaxDeliver(); {
	case d <= 0:
		t.Errorf("%s: MaxDeliver() = %d, want > 0; a transient failure would never be retried", name, d)
	case d > maxDeliverCeiling:
		t.Errorf("%s: MaxDeliver() = %d, want <= %d; a budget this large usually means a permanent failure is being retried",
			name, d, maxDeliverCeiling)
	}

	if c, ok := h.(consumer.ConcurrentHandler); ok {
		if n := c.MaxConcurrency(); n <= 0 {
			t.Errorf("%s: MaxConcurrency() = %d, want > 0; implementing ConcurrentHandler then asking for none stalls the handler", name, n)
		}
	}
}

// verifyDurableNamesUnique is the rule no handler can enforce alone. Two
// handlers sharing a durable name share one JetStream cursor: every message
// goes to whichever asks first, so each sees an arbitrary half of the stream
// and neither reports a problem.
func verifyDurableNamesUnique(t *testing.T, handlers []consumer.EventHandler) {
	t.Helper()

	seen := make(map[string]consumer.EventHandler, len(handlers))
	for _, h := range handlers {
		d := h.DurableName()
		if d == "" {
			continue // already reported by verifyIdentity
		}
		if prev, dup := seen[d]; dup {
			t.Errorf("durable name %q is used by both %s and %s; they would consume each other's messages, each seeing part of the stream",
				d, prev.Name(), h.Name())
			continue
		}
		seen[d] = h
	}
}

// Verdict is what the consumer will do with a message once the handler returns.
type Verdict string

const (
	// Ack: handled successfully, or the effect was already applied.
	Ack Verdict = "ack"
	// Permanent: terminated without redelivery — no retry could succeed.
	Permanent Verdict = "permanent"
	// Retry: redelivered up to MaxDeliver, then dead-lettered.
	Retry Verdict = "retry"
)

// Case is one payload and the verdict it must produce.
type Case struct {
	// Name describes the situation, not the payload: "malformed JSON" rather
	// than "bad bytes". It becomes the subtest name.
	Name string
	// Data is the raw message body, exactly as it arrives over NATS.
	Data []byte
	// Want is the verdict the handler must produce for this payload.
	Want Verdict
	// Header is optional, for handlers that read one.
	Header nats.Header
}

// VerifyClassification runs each case through the handler and asserts the
// resulting verdict.
//
// The question it answers is the one the whole retry mechanism rests on: could
// a redelivery ever change this outcome? Malformed JSON parses the same way
// forever, so retrying it burns the budget and delays the DLQ. A database that
// is down will come back, so terminating loses the message for good. Both are
// "the handler failed"; their consequences are opposite.
//
// It also asserts that the handler returns rather than panics. That is not a
// style preference: the consumer loop has no recover(), so a panic escapes into
// the goroutine and takes the other in-flight messages with it.
//
// Only the handler's author can supply these cases, which is why they are a
// parameter rather than something the harness invents.
func VerifyClassification(t *testing.T, h consumer.EventHandler, cases []Case) {
	t.Helper()

	if len(cases) == 0 {
		t.Fatalf("%s: VerifyClassification called with no cases", h.Name())
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			msg := &nats.Msg{Subject: h.Subject(), Data: c.Data, Header: c.Header}
			if msg.Header == nil {
				msg.Header = nats.Header{}
			}

			// A short deadline keeps a handler that blocks on a real dependency
			// from hanging the suite; it also makes such a handler visible,
			// since a unit-level contract check should touch no network.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := handleWithoutPanic(t, h, ctx, msg)
			assertVerdict(t, err, c.Want)
		})
	}
}

// handleWithoutPanic calls Handle and converts a panic into a test failure
// rather than letting it unwind — which is precisely what the real consumer
// cannot do.
func handleWithoutPanic(t *testing.T, h consumer.EventHandler, ctx context.Context, msg *nats.Msg) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s: Handle panicked (%v); the consumer has no recover(), so this would unwind the goroutine and lose every in-flight message. Return an error instead.",
				h.Name(), r)
		}
	}()
	return h.Handle(ctx, msg)
}

func assertVerdict(t *testing.T, err error, want Verdict) {
	t.Helper()

	got := VerdictOf(err)
	if got == want {
		return
	}

	switch want {
	case Ack:
		t.Errorf("verdict = %s (err: %v), want %s; the handler reports a failure for something it in fact handled", got, err, want)
	case Permanent:
		t.Errorf("verdict = %s (err: %v), want %s; no redelivery can fix this, so it must be wrapped with consumer.Permanent — otherwise it is retried MaxDeliver times before the DLQ takes it anyway",
			got, err, want)
	case Retry:
		t.Errorf("verdict = %s (err: %v), want %s; this failure resolves on its own, so terminating it drops a message a later attempt would have handled",
			got, err, want)
	default:
		t.Errorf("verdict = %s (err: %v), want %s", got, err, want)
	}
}

// VerdictOf reports what the consumer will do with a message, given what the
// handler returned. It mirrors the engine's own dispatch (consumer.go, the
// switch after Handle) case for case and in the same order, so a test asserting
// on it asserts on real behaviour rather than on an error string.
//
// The order matters: AlreadyDone is checked before Permanent, so an error that
// happens to wrap both is an idempotent success — the effect is in place, which
// is what the caller wanted, whatever else the error says.
func VerdictOf(err error) Verdict {
	switch {
	case err == nil:
		return Ack
	case errors.Is(err, consumer.ErrAlreadyDone):
		return Ack
	case errors.Is(err, consumer.ErrPermanent):
		return Permanent
	default:
		return Retry
	}
}

// MustJSON marshals v for use as a Case payload, failing the test rather than
// returning an error — a fixture that will not marshal is a broken test, not a
// scenario worth handling.
func MustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}
