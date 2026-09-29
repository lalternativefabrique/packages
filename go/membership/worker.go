package membership

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RunWorker drives provisioning and erasure until ctx ends. It is the only
// place Resources is called: never from a person's request.
func (s *Service) RunWorker(ctx context.Context, poll time.Duration) {
	if poll <= 0 {
		poll = time.Second
	}
	for {
		n, err := s.WorkOnce(ctx)
		if err != nil && ctx.Err() == nil {
			s.cfg.Logger.ErrorContext(ctx, "membership worker", "product", s.p.Name, "error", err)
		}
		if n > 0 && ctx.Err() == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(poll):
		}
	}
}

// WorkOnce processes one batch of due members and reports how many it took.
func (s *Service) WorkOnce(ctx context.Context) (int, error) {
	due, err := s.store.ClaimDue(ctx, s.cfg.Now(), s.cfg.Lease, s.cfg.BatchSize)
	if err != nil {
		return 0, err
	}
	for _, m := range due {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		s.process(ctx, m)
	}
	return len(due), nil
}

func (s *Service) process(ctx context.Context, m Member) {
	var err error
	switch m.State {
	case StateProvisioning:
		err = s.provision(ctx, m)
	case StateErasing:
		err = s.erase(ctx, m)
	default:
		return
	}
	if err == nil {
		return
	}
	attempts := m.Attempts + 1
	retryAt := s.cfg.Now().Add(s.backoff(attempts))
	if ferr := s.store.Fail(ctx, m.IdentityID, err.Error(), retryAt); ferr != nil {
		s.cfg.Logger.ErrorContext(ctx, "membership: record failure", "identity_id", m.IdentityID, "error", ferr)
	}
	s.cfg.Logger.WarnContext(ctx, "membership step failed", "product", s.p.Name, "identity_id", m.IdentityID, "state", m.State, "attempt", attempts, "error", err)
	if attempts >= s.cfg.MaxAttempts && s.cfg.OnStuck != nil {
		m.Attempts = attempts
		m.LastError = err.Error()
		s.cfg.OnStuck(m, err)
	}
}

func (s *Service) provision(ctx context.Context, m Member) error {
	resources, err := s.p.Resources.Provision(ctx, m)
	if errors.Is(err, ErrConflict) {
		_, terr := s.store.Transition(ctx, m.IdentityID, StateProvisioning, StateConflict, Patch{LastError: err.Error()}, nil)
		if terr != nil {
			return terr
		}
		s.cfg.Logger.ErrorContext(ctx, "member in conflict", "product", s.p.Name, "identity_id", m.IdentityID, "key", m.Key(), "cause", err)
		if s.cfg.OnStuck != nil {
			s.cfg.OnStuck(m, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("provision: %w", err)
	}
	if len(resources) == 0 {
		resources = []byte("{}")
	}
	_, err = s.store.Transition(ctx, m.IdentityID, StateProvisioning, StateReady, Patch{Resources: resources}, nil)
	return err
}

func (s *Service) erase(ctx context.Context, m Member) error {
	if err := s.p.Resources.Erase(ctx, m); err != nil {
		return fmt.Errorf("erase resources: %w", err)
	}
	_, err := s.store.Transition(ctx, m.IdentityID, StateErasing, StateErased, Patch{Resources: []byte("{}")}, func(ctx context.Context, m *Member) error {
		if s.p.Purge == nil {
			return nil
		}
		if err := s.p.Purge(ctx, *m); err != nil {
			return fmt.Errorf("purge: %w", err)
		}
		return nil
	})
	return err
}

func (s *Service) backoff(attempts int) time.Duration {
	d := s.cfg.BaseBackoff
	for i := 1; i < attempts; i++ {
		d *= 2
		if d >= s.cfg.MaxBackoff {
			return s.cfg.MaxBackoff
		}
	}
	return d
}
