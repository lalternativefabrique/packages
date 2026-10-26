// Package synthesis is the engine Synthiz and L'Alter both write a source's
// comprehension sheet with, so the same source gives the same sheet on either
// side and a sheet one of them paid for serves the other from cache.
package synthesis

// EngineVersion names the skills and rules that wrote a sheet. Bump it when a
// change to them would make an older cached sheet differ from a new one.
const EngineVersion = "1"

type Source struct {
	ID    string
	Type  string
	URL   string
	Title string
}

// Segment is a run of the source's text; Start and End are seconds for a
// timecoded source (youtube, podcast) and zero otherwise.
type Segment struct {
	Text  string  `json:"text"`
	Start float64 `json:"start_seconds"`
	End   float64 `json:"end_seconds"`
}

type Content struct {
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	Concepts     []Concept `json:"concepts"`
	Tags         []string  `json:"tags"`
	Development  []Axis    `json:"development"`
	WhyItMatters string    `json:"why_it_matters"`
	Impacts      string    `json:"impacts"`
}

type Concept struct {
	Text      string     `json:"text"`
	Citations []Citation `json:"citations"`
}

type Axis struct {
	Axis      string     `json:"axis"`
	Content   string     `json:"content"`
	Citations []Citation `json:"citations"`
}

type Citation struct {
	SourceID    string  `json:"source_id"`
	LocatorType string  `json:"locator_type"`
	Start       float64 `json:"start"`
	End         float64 `json:"end"`
	Text        string  `json:"text"`
	URL         string  `json:"url"`
}

type chunk struct {
	ID           string
	Content      string
	StartSeconds float64
	EndSeconds   float64
}
