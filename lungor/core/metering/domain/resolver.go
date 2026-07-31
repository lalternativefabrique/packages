package domain

import (
	"context"

	"github.com/google/uuid"
)

// customerNamespace is the fixed UUID namespace used to derive a deterministic
// customer uuid from an app's external user id. The ledger aggregates balances
// by customer_id (a uuid), so the external id (owner_id / user_id, which is not
// a uuid) must map to a STABLE uuid — otherwise a tenant's balance fragments.
var customerNamespace = uuid.MustParse("6f9619ff-8b86-d011-b42d-00c04fc964ff")

// StaticResolver maps every external user to a fixed tenant and a deterministic
// customer uuid, without any customers table. It replaces Lungor's
// Postgres-backed resolver (which joins apps/customers) for the shared-lib
// deployment where those tables do not exist.
type StaticResolver struct {
	TenantID uuid.UUID
}

// NewStaticResolver builds a resolver pinned to one tenant.
func NewStaticResolver(tenantID uuid.UUID) StaticResolver {
	return StaticResolver{TenantID: tenantID}
}

// Resolve returns the fixed tenant and a deterministic customer uuid derived from
// (appID, externalUserID). appID is folded in so the same external id under two
// apps never collides.
func (r StaticResolver) Resolve(_ context.Context, appID uuid.UUID, externalUserID string) (Resolved, error) {
	seed := appID.String() + "\x00" + externalUserID
	return Resolved{
		TenantID:   r.TenantID,
		CustomerID: uuid.NewSHA1(customerNamespace, []byte(seed)),
	}, nil
}
