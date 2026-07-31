package infrastructure

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/lungor/core/metering/domain"
)

type UsageUnitRepo struct{ pool *pgxpool.Pool }

func NewUsageUnitRepo(p *pgxpool.Pool) *UsageUnitRepo { return &UsageUnitRepo{pool: p} }

func (r *UsageUnitRepo) Create(ctx context.Context, u *domain.UsageUnit) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO usage_units (id, tenant_id, app_id, code, name, unit_amount, currency, active, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, u.ID, u.TenantID, u.AppID, u.Code, u.Name, u.UnitAmount, u.Currency, u.Active, u.CreatedAt)
	return err
}

func (r *UsageUnitRepo) GetByCode(ctx context.Context, appID uuid.UUID, code string) (*domain.UsageUnit, error) {
	u := &domain.UsageUnit{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, app_id, code, name, unit_amount, currency, active, created_at
		FROM usage_units WHERE app_id=$1 AND code=$2
	`, appID, code).Scan(&u.ID, &u.TenantID, &u.AppID, &u.Code, &u.Name, &u.UnitAmount, &u.Currency, &u.Active, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrUnitNotFound
	}
	return u, err
}

func (r *UsageUnitRepo) ListByApp(ctx context.Context, appID uuid.UUID) ([]*domain.UsageUnit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, app_id, code, name, unit_amount, currency, active, created_at
		FROM usage_units WHERE app_id=$1 ORDER BY created_at
	`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.UsageUnit
	for rows.Next() {
		u := &domain.UsageUnit{}
		if err := rows.Scan(&u.ID, &u.TenantID, &u.AppID, &u.Code, &u.Name, &u.UnitAmount, &u.Currency, &u.Active, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
