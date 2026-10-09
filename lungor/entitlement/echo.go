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

// Spend debits n of a metered unit on Lungor before the handler runs, keyed on
// the request id, and releases it when the handler fails.
func (g *Gate) Spend(unit string, n int64) echo.MiddlewareFunc {
	g.track(unit)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			subject, err := g.subject(c)
			if err != nil {
				return err
			}
			rid := requestID(c)
			if rid == "" {
				return echo.NewHTTPError(http.StatusInternalServerError).
					SetInternal(errors.New("entitlement: Spend needs a request id; mount echo's RequestID middleware"))
			}
			ctx := c.Request().Context()
			usage := sdk.Usage{ExternalUserID: subject, Unit: unit, Quantity: n, IdempotencyKey: unit + ":" + rid}
			d, err := g.cfg.Ledger.Consume(ctx, usage)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrLedgerUnavailable, err)
			}
			if !d.Allowed {
				return g.spendRefusal(ctx, subject, unit, d)
			}

			herr := next(c)
			if herr != nil || c.Response().Status >= http.StatusInternalServerError {
				usage.IdempotencyKey += ":release"
				if _, err := g.cfg.Ledger.Release(context.WithoutCancel(ctx), usage); err != nil {
					slog.Error("entitlement: spend not released after failed handler", "subject", subject, "unit", unit, "err", err)
				}
			}
			return herr
		}
	}
}

func (g *Gate) spendRefusal(ctx context.Context, subject, unit string, d sdk.Decision) error {
	lerr := &LimitError{Unit: unit}
	if s, err := g.snapshot(ctx, subject); err == nil {
		lerr.Plan, lerr.Limit = s.Plan, s.Limits[unit]
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

// Webhook receives Lungor deliveries and re-reads the subject they name, so a
// plan change applies without waiting for Refresh. A bad signature is 401.
func (g *Gate) Webhook(secret string) echo.HandlerFunc {
	return func(c echo.Context) error {
		body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxWebhookBody))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest).SetInternal(err)
		}
		d, err := sdk.VerifyWebhook(secret, c.Request().Header, body)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized).SetInternal(err)
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

func subjectOf(payload []byte) string {
	var p struct {
		ExternalUserID string `json:"external_user_id"`
		Data           struct {
			ExternalUserID string `json:"external_user_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	if p.ExternalUserID != "" {
		return p.ExternalUserID
	}
	return p.Data.ExternalUserID
}

// ErrorHandler answers *LimitError with 402 and ErrLedgerUnavailable with 503,
// and hands every other error to next.
func ErrorHandler(next echo.HTTPErrorHandler) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		var lerr *LimitError
		switch {
		case errors.As(err, &lerr):
			writeJSON(c, http.StatusPaymentRequired, map[string]any{
				"code": "plan_limit", "unit": lerr.Unit, "limit": lerr.Limit, "used": lerr.Used, "plan": lerr.Plan,
			})
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
