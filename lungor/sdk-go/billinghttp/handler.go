// Package billinghttp serves the subscription-management half of an app's
// billing page: changing plan with the customer's consent, cancelling at the
// period end, resuming, withdrawing a scheduled downgrade, and reading or
// replacing the card on file. Every rule behind those actions lives in Lungor;
// this only authenticates the caller as the app's own user and relays.
//
// Mount it under a prefix and serve the matching React hook
// (useBillingActions in @lalternative/lungor-sdk-react) against that prefix:
//
//	mux.Handle("/api/billing/manage/", http.StripPrefix("/api/billing/manage",
//		billinghttp.New(billinghttp.Config{Client: lungor, UserID: currentUser, ReturnURL: billingPage})))
package billinghttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

// Config wires the handler to the app.
type Config struct {
	Client *sdk.Client
	// UserID returns the authenticated user's id as the app registered it with
	// Lungor, or false to answer 401.
	UserID func(r *http.Request) (string, bool)
	// ReturnURL is where the provider sends the user back after entering a new
	// card: the app's billing page.
	ReturnURL string
}

// New returns the handler. Routes, relative to where it is mounted:
//
//	POST /change-plan        {code, agreed?, agreed_amount_cents?} → quote or outcome
//	POST /cancel             → {effective_at}
//	POST /resume             → 204
//	POST /withdraw-pending   → 204
//	GET  /payment-method     → card
//	POST /payment-method     → {redirect_url}
func New(cfg Config) http.Handler {
	h := &handler{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /change-plan", h.authed(h.changePlan))
	mux.HandleFunc("POST /cancel", h.authed(h.cancel))
	mux.HandleFunc("POST /resume", h.authed(h.resume))
	mux.HandleFunc("POST /withdraw-pending", h.authed(h.withdrawPending))
	mux.HandleFunc("GET /payment-method", h.authed(h.card))
	mux.HandleFunc("POST /payment-method", h.authed(h.replaceCard))
	return mux
}

type handler struct{ cfg Config }

func (h *handler) authed(next func(w http.ResponseWriter, r *http.Request, userID string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := h.cfg.UserID(r)
		if !ok || userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		next(w, r, userID)
	}
}

// Card is all a provider reveals about a saved card.
type Card struct {
	Brand     string `json:"brand"`
	Last4     string `json:"last4"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func cardOf(pm *sdk.PaymentMethod) *Card {
	if pm == nil || pm.Last4 == "" {
		return nil
	}
	c := &Card{Brand: pm.Brand, Last4: pm.Last4}
	if !pm.ExpiresAt.IsZero() {
		c.ExpiresAt = pm.ExpiresAt.Format(time.RFC3339)
	}
	return c
}

type changePlanRequest struct {
	Code              string `json:"code"`
	Agreed            bool   `json:"agreed,omitempty"`
	AgreedAmountCents int64  `json:"agreed_amount_cents,omitempty"`
}

// ChangePlanResponse is either a quote to show (ConsentRequired: nothing
// changed yet, resend with agreed and the amount shown) or what the move did.
type ChangePlanResponse struct {
	ConsentRequired       bool   `json:"consent_required"`
	ConsentAmountCents    int64  `json:"consent_amount_cents,omitempty"`
	ConsentRecurringCents int64  `json:"consent_recurring_cents,omitempty"`
	Card                  *Card  `json:"card,omitempty"`
	AppliedNow            bool   `json:"applied_now"`
	EffectiveAt           string `json:"effective_at,omitempty"`
	ProratedCents         int64  `json:"prorated_cents,omitempty"`
}

func (h *handler) changePlan(w http.ResponseWriter, r *http.Request, userID string) {
	var req changePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	out, err := h.cfg.Client.ChangePlan(r.Context(), sdk.ChangePlanInput{
		ExternalUserID:    userID,
		PlanCode:          req.Code,
		Agreed:            req.Agreed,
		AgreedAmountCents: req.AgreedAmountCents,
	})
	if err != nil && !errors.Is(err, sdk.ErrConsentRequired) {
		writeLungorError(w, err)
		return
	}
	resp := ChangePlanResponse{
		ConsentRequired:       errors.Is(err, sdk.ErrConsentRequired),
		ConsentAmountCents:    out.ConsentAmount,
		ConsentRecurringCents: out.ConsentRecurring,
		Card:                  cardOf(out.PaymentMethod),
		AppliedNow:            out.AppliedNow,
		ProratedCents:         out.ProratedCents,
	}
	if !out.EffectiveAt.IsZero() {
		resp.EffectiveAt = out.EffectiveAt.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) cancel(w http.ResponseWriter, r *http.Request, userID string) {
	out, err := h.cfg.Client.Cancel(r.Context(), userID, true)
	if err != nil {
		writeLungorError(w, err)
		return
	}
	body := map[string]string{}
	if !out.EffectiveAt.IsZero() {
		body["effective_at"] = out.EffectiveAt.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *handler) resume(w http.ResponseWriter, r *http.Request, userID string) {
	if _, err := h.cfg.Client.Resume(r.Context(), userID); err != nil {
		writeLungorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) withdrawPending(w http.ResponseWriter, r *http.Request, userID string) {
	if _, _, err := h.cfg.Client.WithdrawPendingPlan(r.Context(), userID); err != nil {
		writeLungorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) card(w http.ResponseWriter, r *http.Request, userID string) {
	pm, err := h.cfg.Client.CurrentPaymentMethod(r.Context(), userID)
	if err != nil {
		writeLungorError(w, err)
		return
	}
	card := cardOf(&pm)
	if card == nil {
		writeError(w, http.StatusNotFound, "no card on file")
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (h *handler) replaceCard(w http.ResponseWriter, r *http.Request, userID string) {
	page, err := h.cfg.Client.ReplacePaymentMethod(r.Context(), userID, h.cfg.ReturnURL)
	if err != nil {
		writeLungorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect_url": page})
}

// writeLungorError keeps Lungor's own refusals (no subscription, a request it
// rejects) distinct from an outage, so the page can tell "nothing to change"
// from "try again".
func writeLungorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sdk.ErrNotFound):
		writeError(w, http.StatusNotFound, "no active subscription")
	case errors.Is(err, sdk.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, sdk.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusServiceUnavailable, "billing unavailable")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
