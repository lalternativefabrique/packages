package schedule

type LaneDTO struct {
	ID    string `json:"id" validate:"required" enums:"rdv,rappel,ecrit"`
	Label string `json:"label" validate:"required"`
	Color string `json:"color" validate:"required"`
}

type KindDTO struct {
	ID    string `json:"id" validate:"required" enums:"appointment,reminder,note,mail"`
	Label string `json:"label" validate:"required"`
	Lane  string `json:"lane" validate:"required" enums:"rdv,rappel,ecrit"`
}

type BandDTO struct {
	ID       string `json:"id" validate:"required" enums:"morning,afternoon,evening"`
	Label    string `json:"label" validate:"required"`
	FromHour int    `json:"from_hour" validate:"required"`
	ToHour   int    `json:"to_hour" validate:"required"`
}

type MomentDTO struct {
	ID    string `json:"id" validate:"required"`
	Kind  string `json:"kind" validate:"required" enums:"appointment,reminder,note,mail"`
	Lane  string `json:"lane" validate:"required" enums:"rdv,rappel,ecrit"`
	Title string `json:"title" validate:"required"`
	// Start and End are RFC 3339.
	Start  string   `json:"start" validate:"required"`
	End    string   `json:"end" validate:"required"`
	Place  string   `json:"place,omitempty"`
	People []string `json:"people" validate:"required"`
	Detail string   `json:"detail,omitempty"`
	Done   bool     `json:"done,omitempty"`
	// Status is a mail's: waiting for its time, being sent, sent, or failed (detail says why).
	Status      string `json:"status,omitempty" enums:"waiting,sending,sent,failed"`
	StatusLabel string `json:"status_label,omitempty"`
}

// ScheduleDTO is the frise for one zoom: the span shown, how it is drawn,
// and every moment in it.
type ScheduleDTO struct {
	Zoom string `json:"zoom" validate:"required" enums:"day,week,month"`
	// From and To bound the span, RFC 3339, To excluded.
	From     string      `json:"from" validate:"required"`
	To       string      `json:"to" validate:"required"`
	Now      string      `json:"now" validate:"required"`
	DayStart int         `json:"day_start" validate:"required"`
	DayEnd   int         `json:"day_end" validate:"required"`
	Lanes    []LaneDTO   `json:"lanes" validate:"required"`
	Kinds    []KindDTO   `json:"kinds" validate:"required"`
	Bands    []BandDTO   `json:"bands" validate:"required"`
	Moments  []MomentDTO `json:"moments" validate:"required"`
}

type AddMomentRequest struct {
	// Text is the line typed, e.g. « Dentiste jeudi 16h ».
	Text string `json:"text"`
	// Kind is the type picked in the sheet; a word of the line naming another wins.
	Kind     string   `json:"kind,omitempty" enums:"appointment,reminder,note,mail"`
	Title    string   `json:"title,omitempty"`
	Place    string   `json:"place,omitempty"`
	People   []string `json:"people,omitempty"`
	Body     string   `json:"body,omitempty"`
	To       string   `json:"to,omitempty"`
	Subject  string   `json:"subject,omitempty"`
	Timezone string   `json:"timezone,omitempty"`
}

// DraftDTO is how a line was understood, before it is kept.
type DraftDTO struct {
	Kind  string `json:"kind" validate:"required" enums:"appointment,reminder,note,mail"`
	Title string `json:"title" validate:"required"`
	Start string `json:"start" validate:"required"`
	// Timed is true when the line gave the time.
	Timed bool `json:"timed" validate:"required"`
	// Free is true when the time is the nearest free slot.
	Free bool   `json:"free" validate:"required"`
	To   string `json:"to,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
