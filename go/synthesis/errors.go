package synthesis

// PermanentError marks a synthesis failure that is deterministic — retrying the
// exact same input will fail identically (e.g. an HTTP 400 from the LLM: prompt
// too large, malformed input). The integration-events consumer terminates such
// messages (msg.Term) instead of redelivering them MaxDeliver times, which would
// just burn identical LLM calls before hitting the DLQ.
type PermanentError struct {
	Reason string
	Err    error
}

func (e *PermanentError) Error() string {
	if e.Err != nil {
		return "permanent synthesis error: " + e.Reason + ": " + e.Err.Error()
	}
	return "permanent synthesis error: " + e.Reason
}

func (e *PermanentError) Unwrap() error { return e.Err }

// NewPermanentError wraps err as a non-retryable synthesis failure.
func NewPermanentError(reason string, err error) *PermanentError {
	return &PermanentError{Reason: reason, Err: err}
}
