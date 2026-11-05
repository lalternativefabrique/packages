package botguard

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Spend is what one request cost a client.
type Spend struct {
	OverBudget  bool
	OverCeiling bool
}

// Limiter counts requests per client key. MemoryLimiter fits one replica; a
// deployment with several replicas plugs in a shared store.
type Limiter interface {
	Take(ctx context.Context, key string, p Policy) (Spend, error)
}

type buckets struct {
	budget, ceiling *rate.Limiter
	seen            time.Time
}

// MemoryLimiter keeps token buckets in memory and forgets clients idle for
// longer than it takes their buckets to refill.
type MemoryLimiter struct {
	mu        sync.Mutex
	clients   map[string]*buckets
	lastSweep time.Time
	now       func() time.Time
}

func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{clients: map[string]*buckets{}, now: time.Now}
}

func (m *MemoryLimiter) Take(_ context.Context, key string, p Policy) (Spend, error) {
	now := m.now()
	perSecond := rate.Limit(p.PerMinute / 60)
	ceilingPerSecond := perSecond * rate.Limit(p.CeilingFactor)
	ceilingBurst := int(float64(p.Burst) * p.CeilingFactor)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweep(now, p)
	b, ok := m.clients[key]
	if !ok {
		b = &buckets{
			budget:  rate.NewLimiter(perSecond, p.Burst),
			ceiling: rate.NewLimiter(ceilingPerSecond, ceilingBurst),
		}
		m.clients[key] = b
	}
	if b.budget.Limit() != perSecond || b.budget.Burst() != p.Burst {
		b.budget.SetLimitAt(now, perSecond)
		b.budget.SetBurstAt(now, p.Burst)
		b.ceiling.SetLimitAt(now, ceilingPerSecond)
		b.ceiling.SetBurstAt(now, ceilingBurst)
	}
	b.seen = now
	return Spend{
		OverBudget:  !b.budget.AllowN(now, 1),
		OverCeiling: !b.ceiling.AllowN(now, 1),
	}, nil
}

func (m *MemoryLimiter) sweep(now time.Time, p Policy) {
	idle := time.Duration(float64(p.Burst)*p.CeilingFactor/(p.PerMinute/60)) * time.Second
	if idle < time.Minute {
		idle = time.Minute
	}
	if now.Sub(m.lastSweep) < idle {
		return
	}
	m.lastSweep = now
	for k, b := range m.clients {
		if now.Sub(b.seen) > idle {
			delete(m.clients, k)
		}
	}
}
