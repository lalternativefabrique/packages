package outbox

import (
	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
	"github.com/lalternative/packages/go/webhooks/domain/providers"
)

// classify maps an HTTP result to a DeliveryOutcome.
//
//	2xx → ok
//	4xx (except 408 / 429) → permanent (subscriber rejected)
//	408 / 429 / 5xx / network → transient (retry)
func classify(res providers.HTTPResult) aggregate.DeliveryOutcome {
	if res.Err == nil {
		return aggregate.OutcomeOK
	}
	switch {
	case res.StatusCode == 0:
		return aggregate.OutcomeTransient // network / timeout
	case res.StatusCode == 408, res.StatusCode == 429:
		return aggregate.OutcomeTransient
	case res.StatusCode >= 500:
		return aggregate.OutcomeTransient
	case res.StatusCode >= 400:
		return aggregate.OutcomePermanent
	}
	return aggregate.OutcomeTransient
}
