package dispatcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// fakePuller answers Fetch from a script: each call pops the next reply.
type fakePuller struct {
	mu      sync.Mutex
	replies []error
	fetched int
	closed  bool
}

func (p *fakePuller) Fetch(int, ...nats.PullOpt) ([]*nats.Msg, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fetched++
	if len(p.replies) == 0 {
		time.Sleep(5 * time.Millisecond)
		return nil, nats.ErrTimeout
	}
	err := p.replies[0]
	p.replies = p.replies[1:]
	return nil, err
}

func (p *fakePuller) Unsubscribe() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

type fakeSubscriber struct {
	mu    sync.Mutex
	subs  []*fakePuller
	fails int
}

func (s *fakeSubscriber) subscribe(scripts ...[]error) func() (puller, error) {
	return func() (puller, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.fails > 0 {
			s.fails--
			return nil, errors.New("nats: no responders")
		}
		var replies []error
		if len(s.subs) < len(scripts) {
			replies = scripts[len(s.subs)]
		}
		p := &fakePuller{replies: replies}
		s.subs = append(s.subs, p)
		return p, nil
	}
}

func (s *fakeSubscriber) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

func (s *fakeSubscriber) sub(i int) *fakePuller {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subs[i]
}

func (p *fakePuller) state() (fetched int, closed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fetched, p.closed
}

func runDispatcher(t *testing.T, subscribe func() (puller, error)) (context.CancelFunc, <-chan error) {
	t.Helper()
	d := &Dispatcher{
		source:     Source{StreamName: "S", SubjectFilter: "s.>", PublicType: func(string) string { return "" }},
		subscribe:  subscribe,
		minBackoff: 5 * time.Millisecond,
		maxBackoff: 20 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	return cancel, done
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func stop(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after cancel, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// A pull subscription does not survive its server. Once Fetch says the
// subscription is closed, refetching it forever leaves the instance deaf;
// the loop must open a new subscription instead.
func TestRun_ResubscribesWhenTheSubscriptionCloses(t *testing.T) {
	s := &fakeSubscriber{}
	cancel, done := runDispatcher(t, s.subscribe(
		[]error{nats.ErrTimeout, nats.ErrBadSubscription},
		nil,
	))
	waitFor(t, "a second subscription", func() bool { return s.count() == 2 })
	if _, closed := s.sub(0).state(); !closed {
		t.Fatal("the dead subscription must be unsubscribed before a new one is opened")
	}
	waitFor(t, "the new subscription to be pumped", func() bool {
		fetched, _ := s.sub(1).state()
		return fetched > 0
	})
	stop(t, cancel, done)
}

func TestRun_ConnectionClosedAlsoResubscribes(t *testing.T) {
	s := &fakeSubscriber{}
	cancel, done := runDispatcher(t, s.subscribe([]error{nats.ErrConnectionClosed}, nil))
	waitFor(t, "a second subscription", func() bool { return s.count() == 2 })
	stop(t, cancel, done)
}

// Timeouts are the idle heartbeat of a pull subscription, not a failure: they
// must never trigger a resubscription.
func TestRun_TimeoutsKeepTheSubscription(t *testing.T) {
	s := &fakeSubscriber{}
	cancel, done := runDispatcher(t, s.subscribe([]error{nats.ErrTimeout, nats.ErrTimeout, nats.ErrTimeout}))
	waitFor(t, "several fetches", func() bool {
		if s.count() == 0 {
			return false
		}
		fetched, _ := s.sub(0).state()
		return fetched >= 4
	})
	if s.count() != 1 {
		t.Fatalf("subscriptions = %d, want 1", s.count())
	}
	stop(t, cancel, done)
}

// A broker that is not back yet must be retried with a bounded wait, and the
// loop must still leave promptly when ctx is cancelled while waiting.
func TestRun_RetriesSubscribeWithBackoffAndStopsOnCancel(t *testing.T) {
	s := &fakeSubscriber{fails: 3}
	cancel, done := runDispatcher(t, s.subscribe(nil))
	waitFor(t, "a subscription after the failures", func() bool { return s.count() == 1 })
	stop(t, cancel, done)
}

func TestNextBackoffIsCapped(t *testing.T) {
	b := time.Second
	for i := 0; i < 10; i++ {
		b = nextBackoff(b, 30*time.Second)
	}
	if b != 30*time.Second {
		t.Fatalf("backoff = %v, want the 30s cap", b)
	}
}
