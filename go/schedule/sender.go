package schedule

import (
	"context"
	"log/slog"
	"time"

	"github.com/lalternative/packages/go/schedule/domain"
)

// sendBatch bounds how many due mails one tick claims.
const sendBatch = 20

// SendDue sends the mails whose time has come and settles each as sent or
// failed with its reason. A claimed mail is never claimed again.
func (s *Service) SendDue(ctx context.Context, now time.Time) (int, error) {
	if s.mails == nil {
		return 0, nil
	}
	due, err := s.mails.ClaimDue(ctx, now, sendBatch)
	if err != nil {
		return 0, err
	}
	for _, m := range due {
		status, failure := domain.MailSent, ""
		switch {
		case s.sender == nil:
			status, failure = domain.MailFailed, domain.ErrMailNotConnected.Error()
		default:
			if err := s.sender.Send(ctx, m); err != nil {
				status, failure = domain.MailFailed, err.Error()
			}
		}
		if err := s.mails.Settle(ctx, m.ID, status, failure); err != nil {
			return 0, err
		}
		m.Status, m.Failure = status, failure
		if s.onSettled != nil {
			s.onSettled(ctx, m)
		}
	}
	return len(due), nil
}

// Run sends due mails every interval until ctx ends.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if _, err := s.SendDue(ctx, now); err != nil {
				slog.ErrorContext(ctx, "send scheduled mails", "error", err)
			}
		}
	}
}
