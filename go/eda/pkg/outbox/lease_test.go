package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeLeaseStore is an in-memory LeaseStore that models the lease semantics:
// ClaimBatch returns only due rows and leases them into the future; MarkSent
// terminates a row; MarkFailed reschedules it. now is fixed and advanced
// explicitly by tests.
type fakeLeaseStore struct {
	rows []*leaseRow
	now  time.Time
}

type leaseRow struct {
	id        int64
	topic     string
	payload   []byte
	headers   map[string]string
	attempts  int
	sent      bool
	nextAt    time.Time // zero = due immediately
	lastError string
}

func (s *fakeLeaseStore) ClaimBatch(_ context.Context, limit int, lease time.Duration) ([]RawRecord, error) {
	var out []RawRecord
	for _, r := range s.rows {
		if r.sent {
			continue
		}
		if !r.nextAt.IsZero() && r.nextAt.After(s.now) {
			continue // leased / backed off into the future
		}
		r.attempts++
		r.nextAt = s.now.Add(lease) // lease it out
		out = append(out, RawRecord{ID: r.id, Topic: r.topic, Payload: r.payload, Headers: r.headers, Attempts: r.attempts})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *fakeLeaseStore) MarkSent(_ context.Context, id int64) error {
	for _, r := range s.rows {
		if r.id == id {
			r.sent = true
			return nil
		}
	}
	return errors.New("not found")
}

func (s *fakeLeaseStore) MarkFailed(_ context.Context, id int64, cause error, retryAfter time.Duration) error {
	for _, r := range s.rows {
		if r.id == id {
			r.nextAt = s.now.Add(retryAfter)
			if cause != nil {
				r.lastError = cause.Error()
			}
			return nil
		}
	}
	return errors.New("not found")
}

var _ LeaseStore = (*fakeLeaseStore)(nil)

func TestLeaseRelay_ConfigDefaults(t *testing.T) {
	var c LeaseRelayConfig
	c.withDefaults()
	if c.BatchSize != 25 {
		t.Errorf("BatchSize default = %d, want 25", c.BatchSize)
	}
	if c.LeaseDuration != 60*time.Second {
		t.Errorf("LeaseDuration default = %v, want 60s", c.LeaseDuration)
	}
	if c.BaseBackoff != 10*time.Second {
		t.Errorf("BaseBackoff default = %v, want 10s", c.BaseBackoff)
	}
	if c.MaxBackoff != 600*time.Second {
		t.Errorf("MaxBackoff default = %v, want 600s", c.MaxBackoff)
	}
}

func TestLeaseRelay_BackoffForIsExponentialAndCapped(t *testing.T) {
	r := NewLeaseRelay(&fakeLeaseStore{}, RawPublisherFunc(func(context.Context, string, []byte) error { return nil }),
		LeaseRelayConfig{BaseBackoff: 10 * time.Second, MaxBackoff: 600 * time.Second})
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{3, 40 * time.Second},
		{7, 600 * time.Second}, // 10*2^6=640 -> capped
		{0, 10 * time.Second},  // clamped to 1
	}
	for _, tc := range cases {
		if got := r.backoffFor(tc.attempts); got != tc.want {
			t.Errorf("backoffFor(%d) = %v, want %v", tc.attempts, got, tc.want)
		}
	}
}

func TestLeaseRelay_PublishesAndMarksSent(t *testing.T) {
	store := &fakeLeaseStore{
		now:  time.Unix(1700000000, 0),
		rows: []*leaseRow{{id: 1, topic: "integration.x", payload: []byte(`{}`)}},
	}
	var published []string
	r := NewLeaseRelay(store, RawPublisherFunc(func(_ context.Context, topic string, _ []byte) error {
		published = append(published, topic)
		return nil
	}), LeaseRelayConfig{})

	n, err := r.RelayOnce(context.Background())
	if err != nil {
		t.Fatalf("RelayOnce: %v", err)
	}
	if n != 1 || len(published) != 1 || published[0] != "integration.x" {
		t.Fatalf("published = %v (n=%d), want one integration.x", published, n)
	}
	if !store.rows[0].sent {
		t.Errorf("row should be marked sent")
	}
}

// The core lease guarantee: a claimed-but-not-published row stays pending yet
// is leased into the future, so an immediate second claim sees nothing — no
// double publish.
func TestLeaseRelay_ClaimLeasesRowsNoDoublePublish(t *testing.T) {
	store := &fakeLeaseStore{
		now:  time.Unix(1700000000, 0),
		rows: []*leaseRow{{id: 1, topic: "integration.x", payload: []byte(`{}`)}},
	}
	// A publisher that blocks "forever" by failing silently is not needed: we
	// call ClaimBatch directly to observe the lease, mirroring lungor's test.
	claimed, err := store.ClaimBatch(context.Background(), 25, 60*time.Second)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("first claim got %d, want 1", len(claimed))
	}
	if store.rows[0].sent {
		t.Errorf("leased row must not be sent (still pending, just leased)")
	}
	again, err := store.ClaimBatch(context.Background(), 25, 60*time.Second)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second claim got %d, want 0 (row is leased)", len(again))
	}
}

func TestLeaseRelay_PublishFailureSchedulesBackoff(t *testing.T) {
	store := &fakeLeaseStore{
		now:  time.Unix(1700000000, 0),
		rows: []*leaseRow{{id: 1, topic: "integration.x", payload: []byte(`{}`)}},
	}
	r := NewLeaseRelay(store, RawPublisherFunc(func(context.Context, string, []byte) error {
		return errors.New("boom")
	}), LeaseRelayConfig{BaseBackoff: 10 * time.Second, MaxBackoff: 600 * time.Second})

	n, err := r.RelayOnce(context.Background())
	if err != nil {
		t.Fatalf("RelayOnce: %v", err)
	}
	if n != 0 {
		t.Fatalf("published = %d, want 0 on failure", n)
	}
	if store.rows[0].sent {
		t.Errorf("failed row must not be sent")
	}
	if store.rows[0].lastError != "boom" {
		t.Errorf("lastError = %q, want boom", store.rows[0].lastError)
	}
	// attempts was 1 after the claim → backoff = BaseBackoff (10s) into future.
	wantNext := store.now.Add(10 * time.Second)
	if !store.rows[0].nextAt.Equal(wantNext) {
		t.Errorf("nextAt = %v, want %v (10s backoff)", store.rows[0].nextAt, wantNext)
	}
}

type headerPublisher struct {
	gotHeaders map[string]string
	calls      int
}

func (p *headerPublisher) Publish(context.Context, string, []byte) error {
	p.calls++
	return errors.New("the relay must prefer PublishWithHeaders")
}

func (p *headerPublisher) PublishWithHeaders(_ context.Context, _ string, _ []byte, headers map[string]string) error {
	p.calls++
	p.gotHeaders = headers
	return nil
}

func TestLeaseRelay_ForwardsHeadersWhenPublisherSupportsThem(t *testing.T) {
	store := &fakeLeaseStore{
		now: time.Now(),
		rows: []*leaseRow{{
			id: 1, topic: "t", payload: []byte(`{}`),
			headers: map[string]string{"Traceparent": "00-abc-def-01"},
		}},
	}
	pub := &headerPublisher{}
	relay := NewLeaseRelay(store, pub, LeaseRelayConfig{})

	n, err := relay.RelayOnce(context.Background())
	if err != nil {
		t.Fatalf("RelayOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("published %d rows, want 1", n)
	}
	if pub.gotHeaders["Traceparent"] != "00-abc-def-01" {
		t.Errorf("headers = %v, want the row's traceparent", pub.gotHeaders)
	}
}

// A publisher predating headers keeps working untouched.
func TestLeaseRelay_PlainPublisherStillUsed(t *testing.T) {
	store := &fakeLeaseStore{
		now: time.Now(),
		rows: []*leaseRow{{
			id: 1, topic: "t", payload: []byte(`{}`),
			headers: map[string]string{"Traceparent": "00-abc-def-01"},
		}},
	}
	called := 0
	pub := RawPublisherFunc(func(context.Context, string, []byte) error {
		called++
		return nil
	})
	relay := NewLeaseRelay(store, pub, LeaseRelayConfig{})

	if _, err := relay.RelayOnce(context.Background()); err != nil {
		t.Fatalf("RelayOnce: %v", err)
	}
	if called != 1 {
		t.Errorf("plain Publish called %d times, want 1", called)
	}
}
