package metering_test

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lalternative/packages/lungor/core/metering/domain"
)

// memLedger is an in-memory LedgerRepository for unit tests: same semantics as
// the Postgres impl (append-only, idempotent on (appID, key), balance = SUM),
// without a database. It lets the domain/composition be tested in isolation.
type memLedger struct {
	mu      sync.Mutex
	entries []domain.LedgerEntry
	keys    map[string]bool // appID+"\x00"+key -> seen
}

func newMemLedger() *memLedger {
	return &memLedger{keys: map[string]bool{}}
}

func (m *memLedger) idemSeen(appID uuid.UUID, key string) bool {
	return m.keys[appID.String()+"\x00"+key]
}

func (m *memLedger) Append(_ context.Context, e *domain.LedgerEntry) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idemSeen(e.AppID, e.IdempotencyKey) {
		return false, nil
	}
	m.keys[e.AppID.String()+"\x00"+e.IdempotencyKey] = true
	m.entries = append(m.entries, *e)
	return true, nil
}

func (m *memLedger) balance(appID, customerID uuid.UUID, unit string) int64 {
	var b int64
	for _, e := range m.entries {
		if e.AppID == appID && e.CustomerID == customerID && e.Unit == unit {
			b += e.Delta
		}
	}
	return b
}

func (m *memLedger) AppendIfBalance(_ context.Context, e *domain.LedgerEntry) (bool, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bal := m.balance(e.AppID, e.CustomerID, e.Unit)
	if m.idemSeen(e.AppID, e.IdempotencyKey) {
		return false, bal, nil
	}
	if bal+e.Delta < 0 {
		return false, bal, domain.ErrInsufficientBalance
	}
	m.keys[e.AppID.String()+"\x00"+e.IdempotencyKey] = true
	m.entries = append(m.entries, *e)
	return true, bal + e.Delta, nil
}

// consumedIn is the in-memory equivalent of the SQL period sum: absolute total
// of NEGATIVE deltas whose OccurredAt falls in [from, to). Callers hold m.mu.
func (m *memLedger) consumedIn(appID, customerID uuid.UUID, unit string, from, to time.Time) int64 {
	var c int64
	for _, e := range m.entries {
		if e.AppID == appID && e.CustomerID == customerID && e.Unit == unit &&
			e.Delta < 0 && !e.OccurredAt.Before(from) && e.OccurredAt.Before(to) {
			c -= e.Delta
		}
	}
	return c
}

// AppendIfPeriodUnder mirrors the real SQL's semantics (postgres_ledger.go):
// refuse without writing when the window's consumption plus this debit would
// exceed limit; idempotent replays write nothing.
func (m *memLedger) AppendIfPeriodUnder(_ context.Context, e *domain.LedgerEntry, from, to time.Time, limit int64) (bool, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	consumed := m.consumedIn(e.AppID, e.CustomerID, e.Unit, from, to)
	if m.idemSeen(e.AppID, e.IdempotencyKey) {
		return false, consumed, nil
	}
	qty := -e.Delta
	if consumed+qty > limit {
		return false, consumed, domain.ErrQuotaExceeded
	}
	m.keys[e.AppID.String()+"\x00"+e.IdempotencyKey] = true
	m.entries = append(m.entries, *e)
	return true, consumed + qty, nil
}

func (m *memLedger) Balance(_ context.Context, appID, customerID uuid.UUID, unit string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balance(appID, customerID, unit), nil
}

func (m *memLedger) ConsumedBetween(_ context.Context, appID, customerID uuid.UUID, unit string, from, to time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var c int64
	for _, e := range m.entries {
		if e.AppID == appID && e.CustomerID == customerID && e.Unit == unit &&
			e.Delta < 0 && !e.OccurredAt.Before(from) && e.OccurredAt.Before(to) {
			c -= e.Delta
		}
	}
	return c, nil
}

// memUnits is an in-memory UsageUnitRepository.
type memUnits struct {
	mu     sync.Mutex
	byCode map[string]*domain.UsageUnit // appID+"\x00"+code
}

func newMemUnits() *memUnits { return &memUnits{byCode: map[string]*domain.UsageUnit{}} }

func (u *memUnits) Create(_ context.Context, unit *domain.UsageUnit) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.byCode[unit.AppID.String()+"\x00"+unit.Code] = unit
	return nil
}

func (u *memUnits) GetByCode(_ context.Context, appID uuid.UUID, code string) (*domain.UsageUnit, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if v, ok := u.byCode[appID.String()+"\x00"+code]; ok {
		return v, nil
	}
	return nil, domain.ErrUnitNotFound
}

func (u *memUnits) ListByApp(_ context.Context, appID uuid.UUID) ([]*domain.UsageUnit, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []*domain.UsageUnit
	for _, v := range u.byCode {
		if v.AppID == appID {
			out = append(out, v)
		}
	}
	return out, nil
}
