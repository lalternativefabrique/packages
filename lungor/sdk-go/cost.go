package sdk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

var (
	// ErrUnknownCostItem — a line names a code absent from the tenant's cost catalog (404).
	ErrUnknownCostItem = errors.New("lungor: unknown cost item")
	// ErrCostItemNotUsable — the item exists but this app cannot record it:
	// not attached, private to another app, or inactive (422).
	ErrCostItemNotUsable = errors.New("lungor: cost item not usable by this app")
	// ErrNoCostPrice — the item has no price in force at the line's occurred_at (422).
	ErrNoCostPrice = errors.New("lungor: cost item has no price at that date")
)

// CostLine is one cost the app declares, in the item's own unit (tokens for an LLM item).
type CostLine struct {
	Code           string
	Quantity       int64
	OccurredAt     time.Time
	IdempotencyKey string
	ExternalUserID string
	RunKey         string
}

// RecordedCost is a line as Lungor valued it, with the price frozen on the record.
type RecordedCost struct {
	Code               string
	IdempotencyKey     string
	Quantity           int64
	PriceID            string
	PriceMicrosPerUnit int64
	UnitSize           int64
	AmountMicros       int64
	Currency           string
}

// CostResult is the outcome of a RecordCost batch. Accepted counts the lines
// received, Inserted those not already recorded under their idempotency key.
type CostResult struct {
	Accepted int
	Inserted int
	Lines    []RecordedCost
}

// LLMUsage is one LLM call's token counts, declared on the items
// `<Model>.input`, `<Model>.cached` and `<Model>.output`.
type LLMUsage struct {
	Model          string
	InputTokens    int64
	CachedTokens   int64
	OutputTokens   int64
	OccurredAt     time.Time
	ExternalUserID string
	RunKey         string
	IdempotencyKey string
}

// RecordCost declares a batch of costs. The batch is all or nothing: one bad
// line rejects every line, and the error names it.
func (c *Client) RecordCost(ctx context.Context, lines []CostLine) (CostResult, error) {
	if c.baseURL == "" || c.appKey == "" {
		return CostResult{}, ErrNotConfigured
	}
	body, err := costRequest(lines)
	if err != nil {
		return CostResult{}, err
	}
	var out wire.CosttrackingRecordCostsResponse
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.RecordCosts(ctx, body)
	}); err != nil {
		return CostResult{}, costError(err)
	}
	return costResultFrom(out), nil
}

// RecordLLMUsage declares one LLM call as up to three lines, one per non-zero
// token kind, keyed `<IdempotencyKey>.input|.cached|.output`.
func (c *Client) RecordLLMUsage(ctx context.Context, u LLMUsage) (CostResult, error) {
	lines, err := u.lines(time.Now())
	if err != nil {
		return CostResult{}, err
	}
	return c.RecordCost(ctx, lines)
}

func (u LLMUsage) lines(now time.Time) ([]CostLine, error) {
	switch {
	case u.Model == "":
		return nil, fmt.Errorf("%w: empty model", ErrBadRequest)
	case u.IdempotencyKey == "":
		return nil, fmt.Errorf("%w: an idempotency key is required, or a retry double-counts", ErrBadRequest)
	}
	at := u.OccurredAt
	if at.IsZero() {
		at = now
	}
	kinds := []struct {
		kind   string
		tokens int64
	}{{"input", u.InputTokens}, {"cached", u.CachedTokens}, {"output", u.OutputTokens}}
	lines := make([]CostLine, 0, len(kinds))
	for _, k := range kinds {
		if k.tokens == 0 {
			continue
		}
		lines = append(lines, CostLine{
			Code:           u.Model + "." + k.kind,
			Quantity:       k.tokens,
			OccurredAt:     at,
			IdempotencyKey: u.IdempotencyKey + "." + k.kind,
			ExternalUserID: u.ExternalUserID,
			RunKey:         u.RunKey,
		})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: no tokens to record", ErrBadRequest)
	}
	return lines, nil
}

func costRequest(lines []CostLine) (wire.CosttrackingRecordCostsRequest, error) {
	if len(lines) == 0 {
		return wire.CosttrackingRecordCostsRequest{}, fmt.Errorf("%w: no cost lines", ErrBadRequest)
	}
	out := make([]wire.CosttrackingCostLineRequest, 0, len(lines))
	for i, l := range lines {
		switch {
		case l.Code == "":
			return wire.CosttrackingRecordCostsRequest{}, fmt.Errorf("%w: line %d: empty code", ErrBadRequest, i)
		case l.IdempotencyKey == "":
			return wire.CosttrackingRecordCostsRequest{}, fmt.Errorf("%w: line %d: an idempotency key is required", ErrBadRequest, i)
		case l.OccurredAt.IsZero():
			return wire.CosttrackingRecordCostsRequest{}, fmt.Errorf("%w: line %d: occurred_at is required", ErrBadRequest, i)
		case l.Quantity < 0:
			return wire.CosttrackingRecordCostsRequest{}, fmt.Errorf("%w: line %d: negative quantity", ErrBadRequest, i)
		}
		code, key := l.Code, l.IdempotencyKey
		q := int(l.Quantity)
		at := l.OccurredAt.UTC().Format(time.RFC3339Nano)
		w := wire.CosttrackingCostLineRequest{Code: &code, Quantity: &q, OccurredAt: &at, IdempotencyKey: &key}
		if l.ExternalUserID != "" {
			u := l.ExternalUserID
			w.ExternalUserId = &u
		}
		if l.RunKey != "" {
			r := l.RunKey
			w.RunKey = &r
		}
		out = append(out, w)
	}
	return wire.CosttrackingRecordCostsRequest{Lines: &out}, nil
}

func costError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return fmt.Errorf("%w: %s", ErrUnknownCostItem, strings.TrimPrefix(err.Error(), ErrNotFound.Error()+": "))
	case errors.Is(err, ErrUnprocessable):
		detail := strings.TrimPrefix(err.Error(), ErrUnprocessable.Error()+": ")
		if strings.Contains(detail, "has no price in force") {
			return fmt.Errorf("%w: %s", ErrNoCostPrice, detail)
		}
		return fmt.Errorf("%w: %s", ErrCostItemNotUsable, detail)
	}
	return err
}

func costResultFrom(w wire.CosttrackingRecordCostsResponse) CostResult {
	r := CostResult{}
	if w.Accepted != nil {
		r.Accepted = *w.Accepted
	}
	if w.Inserted != nil {
		r.Inserted = *w.Inserted
	}
	if w.Lines != nil {
		r.Lines = make([]RecordedCost, 0, len(*w.Lines))
		for _, l := range *w.Lines {
			rc := RecordedCost{}
			if l.Code != nil {
				rc.Code = *l.Code
			}
			if l.IdempotencyKey != nil {
				rc.IdempotencyKey = *l.IdempotencyKey
			}
			if l.Quantity != nil {
				rc.Quantity = int64(*l.Quantity)
			}
			if l.PriceId != nil {
				rc.PriceID = *l.PriceId
			}
			if l.PriceMicrosPerUnit != nil {
				rc.PriceMicrosPerUnit = int64(*l.PriceMicrosPerUnit)
			}
			if l.UnitSize != nil {
				rc.UnitSize = int64(*l.UnitSize)
			}
			if l.AmountMicros != nil {
				rc.AmountMicros = int64(*l.AmountMicros)
			}
			if l.Currency != nil {
				rc.Currency = *l.Currency
			}
			r.Lines = append(r.Lines, rc)
		}
	}
	return r
}

// CostGroupBy is how CostReport aggregates.
type CostGroupBy string

const (
	CostByItem    CostGroupBy = "item"
	CostByEndUser CostGroupBy = "end_user"
	CostByDay     CostGroupBy = "day"
	CostByMonth   CostGroupBy = "month"
)

// CostReportRow is one group's total, per currency.
type CostReportRow struct {
	Key          string
	Currency     string
	AmountMicros int64
	Quantity     int64
	Records      int
}

// CostReport is the app's own costs over [From, To).
type CostReport struct {
	GroupBy CostGroupBy
	From    time.Time
	To      time.Time
	Rows    []CostReportRow
}

// CostReport sums the calling app's recorded costs over [from, to). Both bounds
// are optional; Lungor defaults to the 1st of the current month until now.
func (c *Client) CostReport(ctx context.Context, groupBy CostGroupBy, from, to *time.Time) (CostReport, error) {
	if c.baseURL == "" || c.appKey == "" {
		return CostReport{}, ErrNotConfigured
	}
	if groupBy == "" {
		return CostReport{}, fmt.Errorf("%w: group_by is required", ErrBadRequest)
	}
	params := &wire.GetAppCostReportParams{GroupBy: string(groupBy)}
	if from != nil {
		s := from.UTC().Format(time.RFC3339)
		params.From = &s
	}
	if to != nil {
		s := to.UTC().Format(time.RFC3339)
		params.To = &s
	}
	var out wire.CosttrackingCostReportView
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetAppCostReport(ctx, params)
	}); err != nil {
		return CostReport{}, err
	}
	return costReportFrom(out), nil
}

func costReportFrom(w wire.CosttrackingCostReportView) CostReport {
	r := CostReport{}
	if w.GroupBy != nil {
		r.GroupBy = CostGroupBy(*w.GroupBy)
	}
	if w.From != nil {
		if t, err := time.Parse(time.RFC3339, *w.From); err == nil {
			r.From = t
		}
	}
	if w.To != nil {
		if t, err := time.Parse(time.RFC3339, *w.To); err == nil {
			r.To = t
		}
	}
	if w.Rows != nil {
		r.Rows = make([]CostReportRow, 0, len(*w.Rows))
		for _, row := range *w.Rows {
			cr := CostReportRow{}
			if row.Key != nil {
				cr.Key = *row.Key
			}
			if row.Currency != nil {
				cr.Currency = *row.Currency
			}
			if row.AmountMicros != nil {
				cr.AmountMicros = int64(*row.AmountMicros)
			}
			if row.Quantity != nil {
				cr.Quantity = int64(*row.Quantity)
			}
			if row.Records != nil {
				cr.Records = *row.Records
			}
			r.Rows = append(r.Rows, cr)
		}
	}
	return r
}
