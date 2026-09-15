package authz

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

// EvaluationPath is where an AuthZEN PDP answers.
const EvaluationPath = "/access/v1/evaluation"

var ErrUnavailable = errors.New("authz: decision point unavailable")

type httpPDP struct {
	url    string
	token  string
	client *http.Client
}

// HTTP is a PDP reached over the AuthZEN Evaluation API at baseURL. token,
// when set, is sent as a bearer. A transport failure or a non-2xx answer is
// ErrUnavailable: the caller must not read it as a denial.
func HTTP(baseURL, token string, client *http.Client) PDP {
	if client == nil {
		client = http.DefaultClient
	}
	return &httpPDP{url: strings.TrimRight(baseURL, "/") + EvaluationPath, token: token, client: client}
}

func (p *httpPDP) Evaluate(ctx context.Context, req Request) (Decision, error) {
	body, err := json.Marshal(toWire(req))
	if err != nil {
		return Decision{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		hr.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(hr)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Decision{}, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	var w wireResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&w); err != nil {
		return Decision{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return decisionFromWire(w), nil
}

// Handler serves pdp over the AuthZEN Evaluation API. Mount it at
// EvaluationPath; authenticating the caller is the mounting code's job.
func Handler(pdp PDP) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var wr wireRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&wr); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		d, err := pdp.Evaluate(r.Context(), fromWire(wr))
		if err != nil {
			http.Error(w, "evaluation failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(decisionToWire(d))
	})
}
