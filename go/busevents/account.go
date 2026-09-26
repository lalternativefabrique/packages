package busevents

import "fmt"

// Published by every product, each under its own events.<product>. prefix.
const (
	AccountSignedUp  = "account.signed_up"
	AccountActivated = "account.activated"
)

// SignedUp is published once a person proved their address on the product:
// @lalternative/auth's onAccountOpened.
type SignedUp struct {
	Meta
	// PersonID is the product's id for the person, the external_user_id it
	// gives Lungor.
	PersonID string `json:"person_id"`
	// Source is the UTM source or the referrer host, when known.
	Source string `json:"source,omitempty"`
}

func (e SignedUp) Validate() error {
	if err := e.validate(); err != nil {
		return err
	}
	return requirePerson(e.PersonID)
}

// Activated is published the first time a person reaches the product's first
// value, once per person. Trigger is the product's name for that moment.
type Activated struct {
	Meta
	PersonID string `json:"person_id"`
	Trigger  string `json:"trigger"`
}

func (e Activated) Validate() error {
	if err := e.validate(); err != nil {
		return err
	}
	if e.Trigger == "" {
		return fmt.Errorf("%w: no trigger", ErrInvalid)
	}
	return requirePerson(e.PersonID)
}

func requirePerson(id string) error {
	if id == "" {
		return fmt.Errorf("%w: no person_id", ErrInvalid)
	}
	return nil
}
