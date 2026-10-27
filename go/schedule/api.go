package schedule

import (
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	reminderdomain "github.com/lalternative/packages/go/reminder/domain"

	"github.com/lalternative/packages/go/schedule/domain"
)

const dateLayout = "2006-01-02"

func (s *Service) RegisterRoutes(g *echo.Group) {
	g.GET("/schedule", s.Schedule)
	g.POST("/schedule/understand", s.UnderstandMoment)
	g.POST("/schedule/moments", s.AddMoment)
}

// Schedule godoc
// @Summary  The frise of one day, week or month
// @Description Rendez-vous, reminders, notes and scheduled mails in the span, soonest first, with the lanes, types and bands to draw them.
// @Tags     schedule
// @Produce  json
// @Param    zoom      query  string  false  "day, week or month; defaults to week"
// @Param    date      query  string  false  "A day in the span, YYYY-MM-DD; defaults to today"
// @Param    timezone  query  string  false  "IANA zone, e.g. Europe/Paris; defaults to UTC"
// @Success  200  {object}  ScheduleDTO
// @Failure  400  {object}  ErrorResponse
// @Security BearerAuth
// @Router   /schedule [get]
// @ID       getSchedule
func (s *Service) Schedule(c echo.Context) error {
	loc, err := zone(c.QueryParam("timezone"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "unknown timezone"})
	}
	now := time.Now().In(loc)
	anchor := now
	if d := c.QueryParam("date"); d != "" {
		if anchor, err = time.ParseInLocation(dateLayout, d, loc); err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "date is YYYY-MM-DD"})
		}
	}
	zoom := domain.Zoom(c.QueryParam("zoom"))
	if zoom == "" {
		zoom = domain.ZoomWeek
	}
	from, to, err := domain.Window(zoom, anchor, loc)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}
	moments, err := s.Between(c.Request().Context(), s.currentUserID(c), from, to)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	out := ScheduleDTO{
		Zoom: string(zoom), From: from.Format(time.RFC3339), To: to.Format(time.RFC3339), Now: now.Format(time.RFC3339),
		DayStart: domain.DayStartHour, DayEnd: domain.DayEndHour,
		Lanes: []LaneDTO{}, Kinds: []KindDTO{}, Bands: []BandDTO{}, Moments: make([]MomentDTO, 0, len(moments)),
	}
	for _, l := range domain.Lanes() {
		out.Lanes = append(out.Lanes, LaneDTO{ID: string(l.ID), Label: l.Label, Color: l.Color})
	}
	for _, k := range domain.Kinds() {
		out.Kinds = append(out.Kinds, KindDTO{ID: string(k.ID), Label: k.Label, Lane: string(k.Lane)})
	}
	for _, b := range domain.Bands() {
		out.Bands = append(out.Bands, BandDTO{ID: b.ID, Label: b.Label, FromHour: b.FromHour, ToHour: b.ToHour})
	}
	for _, m := range moments {
		out.Moments = append(out.Moments, momentDTO(m, loc))
	}
	return c.JSON(http.StatusOK, out)
}

// UnderstandMoment godoc
// @Summary  How a typed line is understood, without keeping it
// @Description The type, title and time the line gives; with no time, the nearest free one.
// @Tags     schedule
// @Accept   json
// @Produce  json
// @Param    body  body      AddMomentRequest  true  "The line and the sheet's fields"
// @Success  200   {object}  DraftDTO
// @Failure  400   {object}  ErrorResponse
// @Failure  409   {object}  ErrorResponse
// @Security BearerAuth
// @Router   /schedule/understand [post]
// @ID       understandMoment
func (s *Service) UnderstandMoment(c echo.Context) error {
	req, loc, err := bind(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}
	d, err := s.Understand(c.Request().Context(), s.currentUserID(c), req, time.Now(), loc)
	if err != nil {
		return refuse(c, err)
	}
	return c.JSON(http.StatusOK, DraftDTO{
		Kind: string(d.Kind), Title: d.Title, Start: d.Placement.Start.In(loc).Format(time.RFC3339),
		Timed: d.Placement.Timed, Free: d.Placement.Free, To: d.To,
	})
}

// AddMoment godoc
// @Summary  Keep a moment typed in one line
// @Description A rendez-vous or a scheduled mail is kept here, a reminder or a note in its own context. A mail with no time is sent through messag at once (409 when messag is not connected or may not send, 429 past the daily limit); with a time it waits for the core's sender.
// @Tags     schedule
// @Accept   json
// @Produce  json
// @Param    body  body      AddMomentRequest  true  "The line and the sheet's fields"
// @Success  201   {object}  MomentDTO
// @Failure  400   {object}  ErrorResponse
// @Failure  409   {object}  ErrorResponse
// @Failure  429   {object}  ErrorResponse
// @Security BearerAuth
// @Router   /schedule/moments [post]
// @ID       addMoment
func (s *Service) AddMoment(c echo.Context) error {
	req, loc, err := bind(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}
	m, err := s.Add(c.Request().Context(), s.currentUserID(c), req, time.Now(), loc)
	if err != nil {
		return refuse(c, err)
	}
	return c.JSON(http.StatusCreated, momentDTO(m, loc))
}

var errBadBody = errors.New("invalid request body")

func bind(c echo.Context) (AddRequest, *time.Location, error) {
	var body AddMomentRequest
	if err := c.Bind(&body); err != nil {
		return AddRequest{}, nil, errBadBody
	}
	loc, err := zone(body.Timezone)
	if err != nil {
		return AddRequest{}, nil, errors.New("unknown timezone")
	}
	return AddRequest{
		Text: body.Text, Kind: domain.Kind(body.Kind), Title: body.Title, Place: body.Place, People: body.People,
		Body: body.Body, To: body.To, Subject: body.Subject,
	}, loc, nil
}

func refuse(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrMailNotConnected), errors.Is(err, domain.ErrMailCannotSend):
		return c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrMailLimit):
		return c.JSON(http.StatusTooManyRequests, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrNoFreeSlot):
		return c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
	case errors.Is(err, ErrUnavailable):
		return c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrTitleMissing), errors.Is(err, domain.ErrEndBeforeStart), errors.Is(err, domain.ErrRecipientMissing), errors.Is(err, domain.ErrRecipientInvalid),
		errors.Is(err, reminderdomain.ErrBodyMissing), errors.Is(err, reminderdomain.ErrDueInPast):
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}
	return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
}

func momentDTO(m domain.Moment, loc *time.Location) MomentDTO {
	people := m.People
	if people == nil {
		people = []string{}
	}
	return MomentDTO{
		ID: m.ID, Kind: string(m.Kind), Lane: string(m.Kind.Lane()), Title: m.Title,
		Start: m.Start.In(loc).Format(time.RFC3339), End: m.End.In(loc).Format(time.RFC3339),
		Place: m.Place, People: people, Detail: m.Detail, Done: m.Done, Status: string(m.Status), StatusLabel: m.Status.Label(),
	}
}

func zone(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(name)
}

func (s *Service) currentUserID(c echo.Context) string {
	if s.userID == nil {
		return ""
	}
	return s.userID(c)
}
