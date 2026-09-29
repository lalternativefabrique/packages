package membership

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Patch is what a transition writes besides the state. A nil Resources keeps
// the current value.
type Patch struct {
	Resources     json.RawMessage
	LastError     string
	NextAttemptAt time.Time
}

// Within runs inside the store's transaction, so a product's own writes
// commit with the member's. A nil Within is allowed everywhere.
type Within func(ctx context.Context, m *Member) error

// Store persists members. The pgx sub-package is the production one; Memory
// serves tests and a product's own test suite.
type Store interface {
	Get(ctx context.Context, identityID string) (Member, error)
	AddressTaken(ctx context.Context, address string) (bool, error)
	// Insert adds the member and returns it, or returns the existing member of
	// that identity untouched. within runs before the insert, in its
	// transaction, and may change the member it receives.
	Insert(ctx context.Context, m Member, within Within) (Member, error)
	// Transition moves the member from one state to another in one write,
	// answering false when it was not in `from`. The store stamps ReadyAt on
	// a move to ready and ErasedAt on a move to erased, and resets Attempts
	// and LastError on both.
	// within runs in the same transaction once the state has moved.
	Transition(ctx context.Context, identityID string, from, to State, patch Patch, within Within) (bool, error)
	// ClaimDue hands out the members in provisioning or erasing whose
	// NextAttemptAt has passed, pushing it by lease so no other worker takes
	// them meanwhile.
	ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Member, error)
	// Fail records one failed attempt and when to try again.
	Fail(ctx context.Context, identityID string, cause string, retryAt time.Time) error
	List(ctx context.Context) ([]Member, error)
}

type Memory struct {
	mu      sync.Mutex
	members map[string]Member
}

func NewMemory() *Memory { return &Memory{members: map[string]Member{}} }

func (s *Memory) Get(_ context.Context, identityID string) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[identityID]
	if !ok {
		return Member{}, ErrNotMember
	}
	return m, nil
}

func (s *Memory) AddressTaken(_ context.Context, address string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.members {
		if m.Address == address {
			return true, nil
		}
	}
	return false, nil
}

func (s *Memory) Insert(ctx context.Context, m Member, within Within) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.members[m.IdentityID]; ok {
		return existing, nil
	}
	if within != nil {
		if err := within(ctx, &m); err != nil {
			return Member{}, err
		}
	}
	if m.Resources == nil {
		m.Resources = json.RawMessage("{}")
	}
	s.members[m.IdentityID] = m
	return m, nil
}

func (s *Memory) Transition(ctx context.Context, identityID string, from, to State, patch Patch, within Within) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[identityID]
	if !ok || m.State != from {
		return false, nil
	}
	now := time.Now().UTC()
	m.State = to
	m.UpdatedAt = now
	m.Attempts = 0
	m.LastError = patch.LastError
	if patch.Resources != nil {
		m.Resources = patch.Resources
	}
	if !patch.NextAttemptAt.IsZero() {
		m.NextAttemptAt = patch.NextAttemptAt
	}
	switch to {
	case StateReady:
		m.ReadyAt = &now
	case StateErased:
		m.ErasedAt = &now
	}
	if within != nil {
		if err := within(ctx, &m); err != nil {
			return false, err
		}
	}
	s.members[identityID] = m
	return true, nil
}

func (s *Memory) ClaimDue(_ context.Context, now time.Time, lease time.Duration, limit int) ([]Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []Member
	for _, m := range s.members {
		if (m.State == StateProvisioning || m.State == StateErasing) && !m.NextAttemptAt.After(now) {
			due = append(due, m)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].NextAttemptAt.Before(due[j].NextAttemptAt) })
	if len(due) > limit {
		due = due[:limit]
	}
	for i, m := range due {
		m.NextAttemptAt = now.Add(lease)
		s.members[m.IdentityID] = m
		due[i] = m
	}
	return due, nil
}

func (s *Memory) Fail(_ context.Context, identityID string, cause string, retryAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[identityID]
	if !ok {
		return ErrNotMember
	}
	m.Attempts++
	m.LastError = cause
	m.NextAttemptAt = retryAt
	m.UpdatedAt = time.Now().UTC()
	s.members[identityID] = m
	return nil
}

func (s *Memory) List(_ context.Context) ([]Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Member, 0, len(s.members))
	for _, m := range s.members {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IdentityID < out[j].IdentityID })
	return out, nil
}
