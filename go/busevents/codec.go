package busevents

import (
	"encoding/json"
	"fmt"
)

// Encode validates e and returns the body to publish, with EventID to send
// as the Nats-Msg-Id header.
func Encode(e Event) (body []byte, msgID string, err error) {
	if err := e.Validate(); err != nil {
		return nil, "", err
	}
	body, err = json.Marshal(e)
	if err != nil {
		return nil, "", fmt.Errorf("encode bus event: %w", err)
	}
	return body, e.ID(), nil
}

// Decode reads and validates a payload of type T. A consumer should treat
// its error as permanent: no redelivery repairs a malformed event.
func Decode[T Event](body []byte) (T, error) {
	var e T
	if err := json.Unmarshal(body, &e); err != nil {
		return e, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := e.Validate(); err != nil {
		return e, err
	}
	return e, nil
}
