package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ProductError is a product refusing a relayed call with
// {"error":{"code","message"}}.
type ProductError struct {
	Product string
	Status  int
	Code    string
	Message string
}

func (e *ProductError) Error() string {
	return fmt.Sprintf("%s answered %d %s: %s", e.Product, e.Status, e.Code, e.Message)
}

type ResourceReader interface {
	Resources(ctx context.Context, p Product, access string) (Grant, error)
}

type ProductClient struct {
	HTTP *http.Client
}

func (pc ProductClient) Resources(ctx context.Context, p Product, access string) (Grant, error) {
	var g Grant
	err := pc.Do(ctx, p, access, http.MethodGet, "/connect/v1/resources", nil, &g)
	var pe *ProductError
	if errors.As(err, &pe) {
		return Grant{}, fmt.Errorf("%w: %s answered %d", ErrUnavailable, p.ID, pe.Status)
	}
	return g, err
}

// Do sends in as JSON to the product's path with the access token and decodes
// a 2xx answer into out. A 401 is ErrTokenRefused, a 5xx or a transport error
// ErrUnavailable, any other refusal a *ProductError.
func (pc ProductClient) Do(ctx context.Context, p Product, access, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.CoreURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := httpClient(pc.HTTP).Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, p.ID, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return ErrTokenRefused
	case res.StatusCode >= 500:
		return fmt.Errorf("%w: %s answered %d", ErrUnavailable, p.ID, res.StatusCode)
	case res.StatusCode < 200 || res.StatusCode > 299:
		return productError(p.ID, res.StatusCode, raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: %s answer unreadable", ErrUnavailable, p.ID)
	}
	return nil
}

func productError(product string, status int, raw []byte) error {
	var body struct {
		Error json.RawMessage `json:"error"`
	}
	pe := &ProductError{Product: product, Status: status}
	if json.Unmarshal(raw, &body) == nil && len(body.Error) > 0 {
		var detail struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body.Error, &detail) == nil {
			pe.Code, pe.Message = detail.Code, detail.Message
		} else {
			_ = json.Unmarshal(body.Error, &pe.Code)
		}
	}
	return pe
}

// Step is what to do once a product or urbangate answered.
type Step int

const (
	StepDone Step = iota
	StepRefreshAndRetry
	StepDisconnected
	StepOutage
)

// AfterProductCall decides on a product's answer. A cached access token the
// product refuses may just have been revoked with an earlier refresh; only a
// token freshly refreshed and still refused says the grant is gone.
func AfterProductCall(err error, cachedToken bool) Step {
	switch {
	case err == nil:
		return StepDone
	case errors.Is(err, ErrTokenRefused) && cachedToken:
		return StepRefreshAndRetry
	case errors.Is(err, ErrTokenRefused):
		return StepDisconnected
	default:
		return StepOutage
	}
}

// AfterRefresh decides on urbangate's answer to a refresh.
func AfterRefresh(err error) Step {
	switch {
	case err == nil:
		return StepDone
	case errors.Is(err, ErrGrantRefused):
		return StepDisconnected
	default:
		return StepOutage
	}
}
