package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

type EndpointReaderPG struct {
	pool *pgxpool.Pool
}

func NewEndpointReaderPG(pool *pgxpool.Pool) *EndpointReaderPG {
	return &EndpointReaderPG{pool: pool}
}

func (r *EndpointReaderPG) List(ctx context.Context, tenantID string, limit, offset int) ([]repository.EndpointView, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, url, event_types, description, status, created_at, updated_at
		  FROM webhook_endpoints
		 WHERE tenant_id = $1 AND status <> 'deleted'
	  ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]repository.EndpointView, 0)
	for rows.Next() {
		var v repository.EndpointView
		if err := rows.Scan(&v.ID, &v.TenantID, &v.URL, &v.EventTypes, &v.Description, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *EndpointReaderPG) Get(ctx context.Context, tenantID, id string) (*repository.EndpointView, error) {
	var v repository.EndpointView
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, url, event_types, description, status, created_at, updated_at
		  FROM webhook_endpoints
		 WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, tenantID).Scan(&v.ID, &v.TenantID, &v.URL, &v.EventTypes, &v.Description, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return &v, nil
}
