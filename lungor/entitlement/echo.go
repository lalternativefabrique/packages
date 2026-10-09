package entitlement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

const maxWebhookBody = 1 << 20

// Require admits the request when the subject's plan allocates at least one
// of unit: a feature flag.
func (g *Gate) Require(unit string) echo.MiddlewareFunc {
	g.track(unit)
	return g.middleware(func(c echo.Context, subject string) error {
		return g.allowFeature(c.Request().Context(), subject, unit)
	})
}

// RequireRoom admits the request when the subject holds fewer than limit-n of
// unit. It panics when no Counter is declared for unit.
func (g *Gate) RequireRoom(unit string, n int64) echo.MiddlewareFunc {
	return g.RequireRoomFrom(unit, func(echo.Context) (int64, error) { return n, nil })
}

// RequireRoomFrom is RequireRoom with the amount read from the request.
func (g *Gate) RequireRoomFrom(unit string, amount func(c echo.Context) (int64, error)) echo.MiddlewareFunc {
	if _, ok := g.cfg.Counters[unit]; !ok {
		panic(fmt.Sprintf("entitlement: no Counter declared for unit %q", unit))
	}
	g.track(unit)
	return g.middleware(func(c echo.Context, subject string) error {
		n, err := amount(c)
		if err != nil {
			return err
		}
		return g.Allow(c.Request().Context(), subject, unit, n)
	})
}

// SpendOpts tunes Spend.
type SpendOpts struct {
	// Key derives the idempotency key from the request. Default: the request
	// id, which needs echo's RequestID middleware.
	Key func(c echo.Context) string
}

// Spend debits n of a metered unit on Lungor before the handler runs, keyed on
// the request id, and releases it when the handler fails.
func (g *Gate) Spend(unit string, n int64) echo.MiddlewareFunc {
	return g.SpendWith(unit, n, SpendOpts{})
}

// SpendWith is Spend with options.
func (g *Gate) SpendWith(unit string, n int64, opts SpendOpts) echo.MiddlewareFunc {
	g.track(unit)
	key := opts.Key
	if key == nil {
		key = requestID
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			subject, err := g.subject(c)
			if err != nil {
				return err
			}
			ctx := c.Request().Context()
			if ok, err := g.passes(ctx, subject); err != nil || ok {
				if err != nil {
					return err
				}
				return next(c)
			}
			k := key(c)
			if k == "" {
				return echo.NewHTTPError(http.StatusInternalServerError).
					SetInternal(errors.New("entitlement: Spend needs an idempotency key; mount echo's RequestID middleware or set SpendOpts.Key"))
			}
			usage := sdk.Usage{ExternalUserID: subject, Unit: unit, Quantity: n, IdempotencyKey: unit + ":" + k}
			d, err := g.consume(ctx, subject, unit, n, usage.IdempotencyKey)
			if err != nil {
				return err
			}
			if !d.Allowed {
				return g.spendRefusal(ctx, subject, unit, d)
			}

			herr := next(c)
			if herr != nil || c.Response().Status >= http.StatusInternalServerError {
				usage.IdempotencyKey += ":release"
				if _, err := g.ledgerFor(subject).Release(context.WithoutCancel(ctx), usage); err != nil {
					slog.Error("entitlement: spend not released after failed handler", "subject", subject, "unit", unit, "err", err)
				}
			}
			return herr
		}
	}
}

// Bypassed reports whether the request's subject skips every check: dev mode,
// or Config.Bypass says so. For handlers that gate something themselves.
func (g *Gate) Bypassed(c echo.Context) (bool, error) {
	subject, err := g.subject(c)
	if err != nil {
		return false, err
	}
	return g.passes(c.Request().Context(), subject)
}

func (g *Gate) spendRefusal(ctx context.Context, subject, unit string, d sdk.Decision) error {
	lerr := &LimitError{Unit: unit}
	if s, err := g.snapshot(ctx, subject); err == nil {
		lerr.Plan, lerr.Limit = s.Plan, g.limitOf(s, unit)
		lerr.Used = max(lerr.Limit-d.Balance, 0)
	}
	return lerr
}

func (g *Gate) middleware(check func(c echo.Context, subject string) error) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			subject, err := g.subject(c)
			if err != nil {
				return err
			}
			if err := check(c, subject); err != nil {
				return err
			}
			return next(c)
		}
	}
}

func (g *Gate) subject(c echo.Context) (string, error) {
	if g.cfg.Subject == nil {
		return "", echo.NewHTTPError(http.StatusInternalServerError).SetInternal(errors.New("entitlement: no Subject configured"))
	}
	subject, err := g.cfg.Subject(c)
	if err != nil || subject == "" {
		return "", echo.NewHTTPError(http.StatusUnauthorized).SetInternal(err)
	}
	return subject, nil
}

func requestID(c echo.Context) string {
	if id := c.Response().Header().Get(echo.HeaderXRequestID); id != "" {
		return id
	}
	return c.Request().Header.Get(echo.HeaderXRequestID)
}

// WebhookOption tunes Webhook.
type WebhookOption func(*webhookOpts)

type webhookOpts struct {
	onDelivery func(ctx context.Context, d sdk.Delivery) error
}

// OnDelivery runs fn on every verified delivery, before the gate re-reads the
// subject. An error answers 500 so Lungor retries the delivery.
func OnDelivery(fn func(ctx context.Context, d sdk.Delivery) error) WebhookOption {
	return func(o *webhookOpts) { o.onDelivery = fn }
}

// Webhook receives Lungor deliveries and re-reads the subject they name, so a
// plan change applies without waiting for Refresh. A bad signature is 401.
func (g *Gate) Webhook(secret string, opts ...WebhookOption) echo.HandlerFunc {
	var o webhookOpts
	for _, opt := range opts {
		opt(&o)
	}
	return func(c echo.Context) error {
		body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxWebhookBody))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest).SetInternal(err)
		}
		d, err := sdk.VerifyWebhook(secret, c.Request().Header, body)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized).SetInternal(err)
		}
		if o.onDelivery != nil {
			if err := o.onDelivery(c.Request().Context(), d); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError).SetInternal(fmt.Errorf("entitlement: webhook %s: %w", d.Type, err))
			}
		}
		if d.Type != sdk.EventEntitlementChanged && !strings.HasPrefix(d.Type, "subscription.") {
			return c.NoContent(http.StatusNoContent)
		}
		if d.Type == sdk.EventEntitlementChanged {
			g.forgetCatalogue()
		}
		subject := subjectOf(d.Payload)
		if subject == "" {
			g.mu.Lock()
			clear(g.cache)
			g.mu.Unlock()
			return c.NoContent(http.StatusNoContent)
		}
		if err := g.Invalidate(c.Request().Context(), subject); err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable).SetInternal(err)
		}
		return c.NoContent(http.StatusNoContent)
	}
}

// subjectOf finds the subject in a delivery, flat or under "data". Lungor
// marshals its finance events in Go field casing; the snake case is kept for
// deliveries shaped by hand.
func subjectOf(payload []byte) string {
	type fields struct {
		Snake string `json:"external_user_id"`
		Go    string `json:"ExternalUserID"`
	}
	var p struct {
		fields
		Data fields `json:"data"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	for _, s := range []string{p.Go, p.Snake, p.Data.Go, p.Data.Snake} {
		if s != "" {
			return s
		}
	}
	return ""
}

// ErrorOption tunes ErrorHandler.
type ErrorOption func(*errorOpts)

type errorOpts struct {
	limitStatus int
	message     func(*LimitError) string
}

// WithLimitStatus sets the status a *LimitError answers with. Default 402.
func WithLimitStatus(status int) ErrorOption {
	return func(o *errorOpts) { o.limitStatus = status }
}

// WithMessage adds a "message" field to the plan_limit body.
func WithMessage(fn func(*LimitError) string) ErrorOption {
	return func(o *errorOpts) { o.message = fn }
}

// ErrorHandler answers *LimitError with 402 and ErrLedgerUnavailable with 503,
// and hands every other error to next.
func ErrorHandler(next echo.HTTPErrorHandler, opts ...ErrorOption) echo.HTTPErrorHandler {
	o := errorOpts{limitStatus: http.StatusPaymentRequired}
	for _, opt := range opts {
		opt(&o)
	}
	return func(err error, c echo.Context) {
		var lerr *LimitError
		switch {
		case errors.As(err, &lerr):
			body := map[string]any{
				"code": "plan_limit", "unit": lerr.Unit, "limit": lerr.Limit, "used": lerr.Used, "plan": lerr.Plan,
			}
			if o.message != nil {
				body["message"] = o.message(lerr)
			}
			writeJSON(c, o.limitStatus, body)
		case errors.Is(err, ErrLedgerUnavailable):
			writeJSON(c, http.StatusServiceUnavailable, map[string]any{"code": "ledger_unavailable"})
		default:
			next(err, c)
		}
	}
}

func writeJSON(c echo.Context, status int, body map[string]any) {
	if c.Response().Committed {
		return
	}
	if err := c.JSON(status, body); err != nil {
		slog.Error("entitlement: write error response", "err", err)
	}
}
