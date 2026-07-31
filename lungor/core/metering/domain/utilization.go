package domain

// Utilization is how much of a granted allocation was actually consumed.
//
// The question it answers is a pricing question, not an engineering one: a tier
// that everyone saturates is underpriced, and one nobody approaches is either
// overpriced or sold to the wrong people. Neither shows up in revenue until it
// is too late to react cheaply.
type Utilization struct {
	// Subscribers counted. Zero is a real answer — a product with no customers
	// has no utilisation, and reporting NaN teaches nobody anything.
	Subscribers int `json:"subscribers"`

	// AveragePercent and MedianPercent of allocation consumed. Both are given
	// because they disagree in the case that matters: a handful of heavy users
	// among many dormant ones pulls the mean up while the median stays low, and
	// that gap IS the mutualisation the pricing depends on.
	AveragePercent float64 `json:"average_percent"`
	MedianPercent  float64 `json:"median_percent"`

	// Consumed and Granted in raw units, so a caller can recompute or aggregate
	// without inheriting this type's rounding.
	Consumed int64 `json:"consumed"`
	Granted  int64 `json:"granted"`
}

// PlanUtilization is one plan's utilisation. Plans are compared, not summed:
// the interesting signal is a single tier drifting toward saturation while the
// others sit idle.
type PlanUtilization struct {
	Plan string `json:"plan"`
	Utilization
}

// Bucket is a share of subscribers inside a consumption band.
//
// A mean alone hides the shape. Two products can both average 40% — one where
// everybody sits near 40, another where half the subscribers are at 5 and half
// are at 80. The first is priced correctly; the second is two products wearing
// one price.
type Bucket struct {
	// Label of the band, e.g. "0-25%". Rendered as given rather than derived
	// client-side, so every surface reports the same bands.
	Label string `json:"label"`
	// From and To bound the band in percent. To is exclusive except in the last
	// band, which includes everything at or above its floor — a subscriber over
	// their allocation still belongs somewhere.
	From int `json:"from"`
	To   int `json:"to"`
	// Subscribers falling in the band.
	Subscribers int `json:"subscribers"`
}

// UtilizationReport is the whole picture: the headline number, the split by
// plan, and the distribution behind both.
type UtilizationReport struct {
	// Unit the report is about. Utilisation is per-unit by construction —
	// averaging minutes with credits would be meaningless.
	Unit    string            `json:"unit"`
	Overall Utilization       `json:"overall"`
	ByPlan  []PlanUtilization `json:"by_plan"`
	Buckets []Bucket          `json:"buckets"`
}
