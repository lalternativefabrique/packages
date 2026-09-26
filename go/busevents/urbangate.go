package busevents

import "fmt"

const Urbangate = "urbangate"

// Published by urbangate under events.urbangate. (its ADR 0006).
const AccountDeletionRequested = "account.deletion_requested"

// DeletionRequested asks every consumer to erase what it holds about one
// person for one product. It is a request, not a fact.
type DeletionRequested struct {
	EventID       string `json:"event_id"`
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
	// IdentityID is the Ory identity id, the sub of every token in the suite.
	IdentityID string `json:"identity_id"`
	// Product names the account being deleted, not the person.
	Product string `json:"product"`
	// UserID is the product's own id for the person.
	UserID string `json:"user_id,omitempty"`
}

func (e DeletionRequested) ID() string { return e.EventID }

func (e DeletionRequested) Validate() error {
	if e.EventID == "" || e.IdentityID == "" || e.Product == "" {
		return fmt.Errorf("%w: event_id, identity_id and product are required", ErrInvalid)
	}
	return nil
}
