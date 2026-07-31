// Package domain is the metering core, extracted from Lungor
// (lungor/core/metering). It is a pure, append-only usage ledger with an atomic
// hard cap on the CURRENT BILLING PERIOD's allowance: the same
// Scope/LedgerEntry/idempotency contract as Lungor, with the SERIALIZABLE check
// moved from the lifetime balance to the period window. Transcript-api and core
// each import it with a fixed AppID and their own usage_ledger table.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrInsufficientBalance is returned by AppendIfBalance when the requested
	// quantity would drive the lifetime balance below zero. No consumption path
	// raises it any more — see AppendIfBalance on why that cap was retired.
	ErrInsufficientBalance = errors.New("insufficient balance")
	// ErrInvalidQuantity guards against zero/negative consumption quantities.
	ErrInvalidQuantity = errors.New("quantity must be positive")
	// ErrQuotaExceeded is returned by a period-bounded Consume when the requested
	// quantity would push the CURRENT PERIOD's consumption past the plan's
	// allowance. It says nothing about the lifetime ledger: a tenant refused this
	// period is served again on renewal.
	ErrQuotaExceeded = errors.New("quota exceeded for the current period")
)

// Scope identifies who is consuming. AppID is a fixed constant per service
// ("techtuel", "synthiz"); ExternalUserID is the service's own id for the end
// user (owner_id / user_id). The tenant and CustomerID are resolved from this
// pair by a CustomerResolver.
type Scope struct {
	AppID          uuid.UUID
	ExternalUserID string
}

// LedgerEntry is an immutable, append-only movement on the usage ledger.
// delta < 0 is a consumption; delta > 0 is a credit. Under the quota model the
// ledger takes DEBITS ONLY on the consumption path — the monthly grant that used
// to write positive deltas is gone, since an allowance is a ceiling compared
// against consumption, not a balance to be filled.
type LedgerEntry struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	AppID          uuid.UUID
	CustomerID     uuid.UUID
	SubscriptionID *uuid.UUID
	Unit           string
	Delta          int64
	IdempotencyKey string
	OccurredAt     time.Time
}

// Decision is the outcome of a Consume call.
type Decision struct {
	Allowed  bool
	Reason   string // populated when Allowed is false
	Balance  int64  // balance AFTER the movement (Consume) or current (CanConsume)
	Recorded bool   // true if a new ledger entry was written (false on idempotent replay)
}
