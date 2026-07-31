package consume

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lalternative/packages/lungor/core/metering/domain"
)

// Input is the generic consumption contract. Quantity is the positive number of
// units to consume. IdempotencyKey is mandatory — apps retry, and the ledger
// dedupes on it.
type Input struct {
	Scope          domain.Scope
	Unit           string
	Quantity       int64
	IdempotencyKey string
	SubscriptionID *uuid.UUID

	// Period is the window the quota is counted over: the cap is what was already
	// consumed inside it plus this debit, against Limit. Mandatory — a debit with
	// no window has no cap to check, and answering "allowed" on a missing period
	// would serve unlimited work for free (NFR4).
	Period *Window
	// Limit is the period's allowance.
	Limit int64
	// OccurredAt stamps the ledger entry. Zero means now. It must agree with the
	// clock that resolved Period, otherwise a debit can land outside the very
	// window it was checked against.
	OccurredAt time.Time
}

// Window is the half-open interval [From, To) a quota is counted over.
type Window struct {
	From time.Time
	To   time.Time
}

type Handler struct {
	units     domain.UsageUnitRepository
	ledger    domain.LedgerRepository
	customers domain.CustomerResolver
}

func NewHandler(units domain.UsageUnitRepository, ledger domain.LedgerRepository, customers domain.CustomerResolver) *Handler {
	return &Handler{units: units, ledger: ledger, customers: customers}
}

// Handle records a consumption (delta = -Quantity). This is a HARD CAP: when the
// debit would overrun the period's allowance it is REFUSED (Allowed=false) and
// nothing is recorded — a customer never overruns their allowance, and is never
// under-billed for a job that was served. The caller checks the decision before
// serving and stops on refusal.
//
// The cap is the consumption already recorded inside Input.Period plus this
// debit, against Limit. Debits from past periods stay on the ledger as the
// accounting record but do not count, so an exhausted period never blocks the
// next one.
//
// Idempotent on IdempotencyKey.
func (h *Handler) Handle(ctx context.Context, in Input) (domain.Decision, error) {
	if in.Quantity <= 0 {
		return domain.Decision{}, domain.ErrInvalidQuantity
	}
	if in.IdempotencyKey == "" {
		return domain.Decision{}, fmt.Errorf("idempotency_key required")
	}
	// Fail closed: no window means no ceiling to check against (NFR4).
	if in.Period == nil {
		return domain.Decision{}, fmt.Errorf("period required")
	}

	// The unit must be declared — keeps the engine generic but billing-safe.
	if _, err := h.units.GetByCode(ctx, in.Scope.AppID, in.Unit); err != nil {
		return domain.Decision{}, err
	}

	res, err := h.customers.Resolve(ctx, in.Scope.AppID, in.Scope.ExternalUserID)
	if err != nil {
		return domain.Decision{}, fmt.Errorf("resolve customer: %w", err)
	}

	occurredAt := in.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}

	entry := &domain.LedgerEntry{
		ID:             uuid.New(),
		TenantID:       res.TenantID,
		AppID:          in.Scope.AppID,
		CustomerID:     res.CustomerID,
		SubscriptionID: in.SubscriptionID,
		Unit:           in.Unit,
		Delta:          -in.Quantity,
		IdempotencyKey: in.IdempotencyKey,
		OccurredAt:     occurredAt.UTC(),
	}

	// Hard cap on the PERIOD: refuse (don't record) when this debit would push the
	// window's consumption past the allowance.
	inserted, consumed, err := h.ledger.AppendIfPeriodUnder(ctx, entry, in.Period.From, in.Period.To, in.Limit)
	if errors.Is(err, domain.ErrQuotaExceeded) {
		return domain.Decision{Allowed: false, Reason: "quota reached for the current period", Balance: in.Limit - consumed}, nil
	}
	if err != nil {
		return domain.Decision{}, err
	}
	// Balance reports what is LEFT in the period.
	return domain.Decision{Allowed: true, Balance: in.Limit - consumed, Recorded: inserted}, nil
}
