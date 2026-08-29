package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/go/reminder/domain"
)

// PostgresRepository stores reminders.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const columns = `id, user_id, body, due_at, status, created_at, fired_at, channels, run_every_seconds`

func (r *PostgresRepository) Save(ctx context.Context, rem *domain.Reminder) error {
	channels, err := json.Marshal(rem.Channels)
	if err != nil {
		return fmt.Errorf("encode channels: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO reminders (id, user_id, body, due_at, status, created_at, channels, run_every_seconds)
		VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7, $8)`,
		rem.ID, rem.UserID, rem.Body, rem.DueAt, string(rem.Status), rem.CreatedAt, channels, runEverySeconds(rem.RunEvery))
	if err != nil {
		return fmt.Errorf("save reminder: %w", err)
	}
	return nil
}

func (r *PostgresRepository) Update(ctx context.Context, rem *domain.Reminder) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE reminders SET body=$2, due_at=$3, status=$4, fired_at=$5, run_every_seconds=$6 WHERE id=$1`,
		rem.ID, rem.Body, rem.DueAt, string(rem.Status), rem.FiredAt, runEverySeconds(rem.RunEvery))
	if err != nil {
		return fmt.Errorf("update reminder: %w", err)
	}
	return nil
}

func (r *PostgresRepository) FindByID(ctx context.Context, id string) (*domain.Reminder, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+columns+` FROM reminders WHERE id=$1`, id)
	rem, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return rem, err
}

func (r *PostgresRepository) List(ctx context.Context, userID string, includeSettled bool) ([]*domain.Reminder, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+` FROM reminders
		WHERE ($1 = '' OR user_id = $1)
		  AND ($2 OR status = 'pending')
		ORDER BY due_at`, userID, includeSettled)
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	defer rows.Close()

	var out []*domain.Reminder
	for rows.Next() {
		rem, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rem)
	}
	return out, rows.Err()
}

// ClaimDue takes what is due and settles the occurrence in the same
// statement. SKIP LOCKED is what stops two pollers delivering the same
// reminder twice. A one-shot reminder is marked fired; a recurring one is
// pushed to its next due date and stays pending, so the next tick can still
// find it.
//
// The next date is computed from NOW() rather than from the elapsed due_at:
// a poller that was down for a while must resume the cadence, not replay
// every occurrence it missed.
func (r *PostgresRepository) ClaimDue(ctx context.Context, limit int) ([]*domain.Reminder, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		UPDATE reminders SET
			fired_at = NOW(),
			status = CASE WHEN run_every_seconds IS NOT NULL THEN status ELSE 'fired' END,
			due_at = CASE WHEN run_every_seconds IS NOT NULL
				THEN NOW() + (run_every_seconds || ' seconds')::interval
				ELSE due_at END
		WHERE id IN (
			SELECT id FROM reminders
			WHERE status='pending' AND due_at <= NOW()
			ORDER BY due_at FOR UPDATE SKIP LOCKED LIMIT $1
		)
		RETURNING `+columns, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due reminders: %w", err)
	}
	defer rows.Close()

	var out []*domain.Reminder
	for rows.Next() {
		rem, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rem)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scan(s scanner) (*domain.Reminder, error) {
	var (
		rem             domain.Reminder
		userID          *string
		status          string
		firedAt         *time.Time
		channels        []byte
		runEverySeconds *int64
	)
	if err := s.Scan(&rem.ID, &userID, &rem.Body, &rem.DueAt, &status,
		&rem.CreatedAt, &firedAt, &channels, &runEverySeconds); err != nil {
		return nil, err
	}
	if userID != nil {
		rem.UserID = *userID
	}
	rem.Status = domain.Status(status)
	rem.FiredAt = firedAt
	if runEverySeconds != nil {
		d := time.Duration(*runEverySeconds) * time.Second
		rem.RunEvery = &d
	}
	if len(channels) > 0 {
		if err := json.Unmarshal(channels, &rem.Channels); err != nil {
			return nil, fmt.Errorf("decode channels: %w", err)
		}
	}
	return &rem, nil
}

// runEverySeconds converts a recurrence interval to what the run_every_seconds
// column stores. Nil means one-shot.
func runEverySeconds(every *time.Duration) *int64 {
	if every == nil {
		return nil
	}
	s := int64(*every / time.Second)
	return &s
}
