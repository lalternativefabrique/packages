package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnitNotFound = errors.New("usage unit not found")
	ErrUnitExists   = errors.New("usage unit already exists for this app/code")
)

// UsageUnit is a meterable unit declared by an app (e.g. "whisper_min",
// "video"). The Code is opaque to the engine: it never knows what it represents,
// only that consumption is accounted in it. Declaration is required before
// Consume accepts the unit — this prevents billing leaks (a quota can only
// reference a known unit).
type UsageUnit struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	AppID    uuid.UUID
	Code     string
	Name     string
	// UnitAmount is the overage / top-up tariff in minor currency units per unit.
	// 0 means the unit is tracked but never billed (pure quota).
	UnitAmount int64
	Currency   string
	Active     bool
	CreatedAt  time.Time
}

// NewUsageUnit builds a unit with a fresh ID, active by default.
func NewUsageUnit(tenantID, appID uuid.UUID, code, name string) *UsageUnit {
	return &UsageUnit{
		ID:        uuid.New(),
		TenantID:  tenantID,
		AppID:     appID,
		Code:      code,
		Name:      name,
		Currency:  "EUR",
		Active:    true,
		CreatedAt: time.Now().UTC(),
	}
}
