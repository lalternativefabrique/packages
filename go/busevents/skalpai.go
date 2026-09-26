package busevents

import "fmt"

const Skalpai = "skalpai"

// Published by Skalpai under events.skalpai.
const AnalyticsDaily = "analytics.daily"

// DailyVisitors counts a product's unique visitors over one UTC day. Visits
// are counted at the source; no per-visitor event crosses the bus.
type DailyVisitors struct {
	Meta
	AppSlug string `json:"app_slug"`
	// Date is the UTC day, YYYY-MM-DD.
	Date     string `json:"date"`
	Visitors int64  `json:"visitors"`
}

func (e DailyVisitors) Validate() error {
	if err := e.validate(); err != nil {
		return err
	}
	if e.AppSlug == "" || len(e.Date) != len("2006-01-02") {
		return fmt.Errorf("%w: app_slug and date are required", ErrInvalid)
	}
	if e.Visitors < 0 {
		return fmt.Errorf("%w: negative visitors", ErrInvalid)
	}
	return nil
}
