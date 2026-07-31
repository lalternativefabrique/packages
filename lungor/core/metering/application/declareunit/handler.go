package declareunit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/lalternative/packages/lungor/core/metering/domain"
)

// Input declares a meterable usage unit for an app. Must be called (once) before
// any Consume of that unit — the consume handler refuses undeclared units.
type Input struct {
	TenantID   uuid.UUID
	AppID      uuid.UUID
	Code       string
	Name       string
	UnitAmount int64  // overage/top-up tariff, minor units. 0 ⇒ track-only.
	Currency   string // defaults to EUR
}

type Handler struct {
	units domain.UsageUnitRepository
}

func NewHandler(units domain.UsageUnitRepository) *Handler {
	return &Handler{units: units}
}

func (h *Handler) Handle(ctx context.Context, in Input) (*domain.UsageUnit, error) {
	if in.Code == "" {
		return nil, fmt.Errorf("code required")
	}
	if in.Name == "" {
		return nil, fmt.Errorf("name required")
	}
	if in.UnitAmount < 0 {
		return nil, fmt.Errorf("unit_amount must be >= 0")
	}

	// Idempotent declaration: an already-declared unit is a no-op success, so
	// seeding units at boot can run on every start.
	if existing, err := h.units.GetByCode(ctx, in.AppID, in.Code); err == nil && existing != nil {
		return existing, nil
	}

	u := domain.NewUsageUnit(in.TenantID, in.AppID, in.Code, in.Name)
	u.UnitAmount = in.UnitAmount
	if in.Currency != "" {
		u.Currency = in.Currency
	}
	if err := h.units.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}
