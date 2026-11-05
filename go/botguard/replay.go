package botguard

import (
	"sync"
	"time"
)

// usedChallenges remembers every solved challenge until it expires, so one
// solution buys exactly one token.
type usedChallenges struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newUsedChallenges() *usedChallenges { return &usedChallenges{seen: map[string]time.Time{}} }

func (u *usedChallenges) claim(signature string, expires, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for s, exp := range u.seen {
		if !now.Before(exp) {
			delete(u.seen, s)
		}
	}
	if _, used := u.seen[signature]; used {
		return false
	}
	u.seen[signature] = expires
	return true
}
