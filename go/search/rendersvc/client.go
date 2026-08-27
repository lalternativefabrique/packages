// Package rendersvc is an HTTP client for a JavaScript-rendering service.
//
// It implements fetch.Renderer against a small standalone service (a single
// POST /render {"url":...} -> {"html":...} endpoint) backed by a headless
// browser such as chromedp. That service is deployed separately — it owns a
// long-lived browser process, which is a deployment concern, not a library
// one — this package only holds the client side of the contract.
package rendersvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client calls a render service.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a Client against a render service at baseURL. Pass nil
// for httpClient to use a client with no default timeout — Render's own
// timeout argument governs each request via the context.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{baseURL: baseURL, httpClient: httpClient}
}

type renderRequest struct {
	URL       string `json:"url"`
	TimeoutMS int64  `json:"timeout_ms"`
}

type renderResponse struct {
	HTML     string `json:"html"`
	FinalURL string `json:"final_url"`
}

// Render implements fetch.Renderer.
func (c *Client) Render(ctx context.Context, url string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(renderRequest{URL: url, TimeoutMS: timeout.Milliseconds()})
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/render", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("render request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("render service returned %d: %s", resp.StatusCode, string(respBody))
	}

	var out renderResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return out.HTML, nil
}
