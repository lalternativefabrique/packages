package membership

import (
	"context"
	"encoding/json"
)

// Noop is the Resources of a product that provisions nothing outside its own
// database: every member is ready as soon as it is opened, and its List is
// every ready member, so reconciliation reports nothing about resources.
type Noop struct{ store Store }

func NewNoop(store Store) Noop { return Noop{store: store} }

func (Noop) Provision(context.Context, Member) (json.RawMessage, error) { return nil, nil }
func (Noop) Erase(context.Context, Member) error                        { return nil }

func (n Noop) List(ctx context.Context) ([]string, error) {
	members, err := n.store.List(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(members))
	for _, m := range members {
		if m.State == StateReady {
			keys = append(keys, m.Key())
		}
	}
	return keys, nil
}
