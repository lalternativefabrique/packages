package audioreader

import "sync"

// reading is one synthesis in progress, and what it produced.
//
// done is closed when the reading finishes; the fields are written before
// that and read only after, so the channel is what publishes them safely
// rather than a lock held for the whole synthesis.
type reading struct {
	done  chan struct{}
	audio []byte
	mime  string
	err   error
}

// inflight collapses concurrent readings of the same text into one.
//
// The cache alone cannot do this: it is filled when a reading ends, so for
// the tens of seconds one takes it is still empty and every listener who
// arrives in the meantime starts a reading of their own. They all pay for the
// same words, and on a voice that synthesizes one utterance at a time they
// queue behind each other — the second listener waits out both.
//
// Per process, so a second replica still reads it once itself. A lock across
// replicas would need a store that offers one, and would fail the request
// when it is unreachable; this costs nothing and removes the common case,
// which is several listeners on one page.
type inflight struct {
	mu   sync.Mutex
	live map[string]*reading
}

func newInflight() *inflight {
	return &inflight{live: map[string]*reading{}}
}

// do runs read for key, or waits for the identical reading already running.
//
// The waiters share the leader's result, error included: a synthesis that
// failed for one would fail for all, and reporting it once is what stops a
// broken voice being called once per listener.
func (f *inflight) do(key string, read func() ([]byte, string, error)) (audio []byte, mime string, err error, shared bool) {
	f.mu.Lock()
	if r, ok := f.live[key]; ok {
		f.mu.Unlock()
		<-r.done
		return r.audio, r.mime, r.err, true
	}
	r := &reading{done: make(chan struct{})}
	f.live[key] = r
	f.mu.Unlock()

	r.audio, r.mime, r.err = read()

	// Dropped before the waiters are released: the next listener must start a
	// new reading rather than join one that is already finished, and by then
	// the cache holds it anyway.
	f.mu.Lock()
	delete(f.live, key)
	f.mu.Unlock()
	close(r.done)

	return r.audio, r.mime, r.err, false
}
