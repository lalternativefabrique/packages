package reminder

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/lalternative/packages/go/reminder/domain"
)

// UserIDFunc extracts the caller's id from the request context. The host app
// supplies this: reminder has no opinion on how auth works.
type UserIDFunc func(c echo.Context) string

// CreateReminderRequest asks to be told something later.
//
// The time is given either as an absolute instant or as a delay from now —
// "in 2h", "45m", "3d". Both are accepted because both are how people say it,
// and an assistant that only took RFC 3339 would make its caller compute what
// it can compute itself.
type CreateReminderRequest struct {
	Body  string `json:"body" example:"check whether the fix landed"`
	DueAt string `json:"due_at,omitempty" example:"2026-08-24T09:00:00Z"`
	In    string `json:"in,omitempty" example:"2h"`
	// Every makes the reminder fire again after this delay instead of once,
	// read the same way as In ("24h", "7d"). Omitted means one-shot.
	Every    string                 `json:"every,omitempty" example:"24h"`
	Channels []domain.ChannelConfig `json:"channels,omitempty"`
}

// UpdateReminderRequest changes what a pending reminder says or how it
// recurs. Fields are omitted rather than nulled: a client sends only what it
// means to change.
type UpdateReminderRequest struct {
	Body  *string `json:"body,omitempty"`
	DueAt *string `json:"due_at,omitempty" example:"2026-08-24T09:00:00Z"`
	// Every, when present, replaces the recurrence: "" clears it back to a
	// one-shot reminder, anything else sets the new interval.
	Every *string `json:"every,omitempty" example:"24h"`
}

// ReminderDTO is a reminder as an API client sees it.
type ReminderDTO struct {
	ID        string                 `json:"id"`
	Body      string                 `json:"body"`
	DueAt     string                 `json:"due_at"`
	Status    string                 `json:"status"`
	CreatedAt string                 `json:"created_at"`
	FiredAt   string                 `json:"fired_at,omitempty"`
	Channels  []domain.ChannelConfig `json:"channels,omitempty"`
	// Every is the recurrence interval as it was given ("24h"). Absent means
	// one-shot.
	Every string `json:"every,omitempty"`
}

// ErrorResponse is what a refused request returns.
type ErrorResponse struct {
	Error string `json:"error"`
}

func (s *Service) CreateReminder(c echo.Context) error {
	var req CreateReminderRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
	}

	due, err := resolveDue(req.DueAt, req.In)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	rem, err := domain.New(s.userID(c), req.Body, due, req.Channels...)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	if req.Every != "" {
		every, err := parseDelay(req.Every)
		if err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		}
		if err := rem.SetRunEvery(&every); err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		}
	}

	if err := s.reminders.Save(c.Request().Context(), rem); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	return c.JSON(http.StatusCreated, toDTO(rem))
}

// UpdateReminder godoc
// @Summary  Change what a pending reminder says or how it recurs
// @Tags     reminders
// @Accept   json
// @Produce  json
// @Param    id    path      string                 true  "Reminder ID"
// @Param    body  body      UpdateReminderRequest  true  "What to change"
// @Success  200   {object}  ReminderDTO
// @Failure  400   {object}  ErrorResponse
// @Failure  404   {object}  ErrorResponse
// @Security BearerAuth
// @Router   /api/v1/reminders/{id} [patch]
// @ID       updateReminder
func (s *Service) UpdateReminder(c echo.Context) error {
	ctx := c.Request().Context()
	rem, err := s.reminders.FindByID(ctx, c.Param("id"))
	if errors.Is(err, domain.ErrNotFound) {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "reminder not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	if rem.UserID != "" && rem.UserID != s.userID(c) {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "reminder not found"})
	}

	var req UpdateReminderRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
	}

	if req.Body != nil {
		if strings.TrimSpace(*req.Body) == "" {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: domain.ErrBodyMissing.Error()})
		}
		rem.Body = strings.TrimSpace(*req.Body)
	}
	if req.DueAt != nil {
		due, err := resolveDue(*req.DueAt, "")
		if err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		}
		rem.DueAt = due
	}
	if req.Every != nil {
		var every *time.Duration
		if *req.Every != "" {
			d, err := parseDelay(*req.Every)
			if err != nil {
				return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			}
			every = &d
		}
		if err := rem.SetRunEvery(every); err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		}
	}

	if err := s.reminders.Update(ctx, rem); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, toDTO(rem))
}

func (s *Service) ListReminders(c echo.Context) error {
	all := c.QueryParam("all") == "true"
	list, err := s.reminders.List(c.Request().Context(), s.userID(c), all)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	out := make([]ReminderDTO, 0, len(list))
	for _, r := range list {
		out = append(out, toDTO(r))
	}
	return c.JSON(http.StatusOK, out)
}

func (s *Service) CancelReminder(c echo.Context) error {
	ctx := c.Request().Context()
	rem, err := s.reminders.FindByID(ctx, c.Param("id"))
	if errors.Is(err, domain.ErrNotFound) {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "reminder not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	// A reminder belongs to whoever asked for it, and someone else's id is
	// not a way to reach it.
	if rem.UserID != "" && rem.UserID != s.userID(c) {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "reminder not found"})
	}

	rem.Cancel()
	if err := s.reminders.Update(ctx, rem); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Service) MarkReminderDone(c echo.Context) error {
	ctx := c.Request().Context()
	rem, err := s.reminders.FindByID(ctx, c.Param("id"))
	if errors.Is(err, domain.ErrNotFound) {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "reminder not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	if rem.UserID != "" && rem.UserID != s.userID(c) {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "reminder not found"})
	}

	rem.Done()
	if err := s.reminders.Update(ctx, rem); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	return c.JSON(http.StatusOK, toDTO(rem))
}

// resolveDue turns either form of "when" into an instant.
//
// A bare duration is accepted with or without a leading "in", because both are
// what a person writes.
func resolveDue(dueAt, in string) (time.Time, error) {
	dueAt, in = strings.TrimSpace(dueAt), strings.TrimSpace(in)
	if dueAt == "" && in == "" {
		return time.Time{}, errors.New("one of due_at or in is required")
	}
	if dueAt != "" && in != "" {
		return time.Time{}, errors.New("due_at and in are mutually exclusive")
	}
	if dueAt != "" {
		t, err := time.Parse(time.RFC3339, dueAt)
		if err != nil {
			return time.Time{}, fmt.Errorf("due_at must be RFC 3339: %v", err)
		}
		return t.UTC(), nil
	}
	d, err := parseDelay(in)
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().UTC().Add(d), nil
}

// parseDelay reads "2h", "in 45m", "3d". Days are handled here because
// time.ParseDuration stops at hours, and a reminder for next week is a normal
// thing to ask for.
func parseDelay(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "in "))
	if days, ok := strings.CutSuffix(s, "d"); ok {
		d, err := time.ParseDuration(days + "h")
		if err != nil {
			return 0, fmt.Errorf("in: %q is not a delay like 2h, 45m or 3d", s)
		}
		return d * 24, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("in: %q is not a delay like 2h, 45m or 3d", s)
	}
	if d <= 0 {
		return 0, errors.New("in: the delay must be positive")
	}
	return d, nil
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

func toDTO(r *domain.Reminder) ReminderDTO {
	dto := ReminderDTO{
		ID: r.ID, Body: r.Body, DueAt: r.DueAt.Format(timeLayout),
		Status: string(r.Status), CreatedAt: r.CreatedAt.Format(timeLayout),
		Channels: r.Channels,
	}
	if r.FiredAt != nil {
		dto.FiredAt = r.FiredAt.Format(timeLayout)
	}
	if r.RunEvery != nil {
		dto.Every = formatDelay(*r.RunEvery)
	}
	return dto
}

// formatDelay renders a duration the way parseDelay reads it back: whole
// days as "Nd", otherwise Go's own compact form ("24h", "45m").
func formatDelay(d time.Duration) string {
	if d > 0 && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return d.String()
}

func (s *Service) userID(c echo.Context) string {
	if s.userIDFn == nil {
		return ""
	}
	return s.userIDFn(c)
}
