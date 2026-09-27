// Package nakoda sends an app's events to nakoda's public API with the app's
// key: sign-ups, activations, subscriptions, daily visitors, erasures. The
// payloads are go/busevents', the contract nakoda reads on its bus too.
package nakoda

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lalternative/packages/go/busevents"
)

const DefaultURL = "https://nakoda.club"

type Config struct {
	// Key is the app's secret nakoda key; NAKODA_KEY when empty.
	Key string
	// URL is nakoda's origin; DefaultURL when empty.
	URL string
	// HTTPClient defaults to one with a 5-second timeout.
	HTTPClient *http.Client
	// Retries is how many times a call nakoda could not answer is sent
	// again; 3 when zero, none when negative.
	Retries int
}

type Client struct {
	key     string
	base    string
	http    *http.Client
	retries int
	now     func() time.Time
	wait    func(context.Context, time.Duration) error
}

var ErrNoKey = errors.New("nakoda: no key (Config.Key or NAKODA_KEY)")

func New(cfg Config) (*Client, error) {
	if cfg.Key == "" {
		cfg.Key = os.Getenv("NAKODA_KEY")
	}
	if cfg.Key == "" {
		return nil, ErrNoKey
	}
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	switch {
	case cfg.Retries == 0:
		cfg.Retries = 3
	case cfg.Retries < 0:
		cfg.Retries = 0
	}
	return &Client{
		key:     cfg.Key,
		base:    strings.TrimRight(cfg.URL, "/"),
		http:    cfg.HTTPClient,
		retries: cfg.Retries,
		now:     func() time.Time { return time.Now().UTC() },
		wait:    sleep,
	}, nil
}

// APIError is nakoda's answer to a call it refused. Temporary tells the
// refusals worth sending again (nakoda unavailable, rate limited) from the
// ones that will be refused again (bad key, invalid payload).
type APIError struct {
	Status int
	Reason string
}

func (e *APIError) Error() string { return fmt.Sprintf("nakoda: %d %s", e.Status, e.Reason) }

func (e *APIError) Temporary() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// Status is what nakoda did with an accepted event: recorded, duplicate (the
// event id was already received), or forgotten (the person was erased).
type Status string

// SignedUp reports a sign-up. EventID defaults to one derived from the
// person, so reporting the same sign-up twice counts it once.
func (c *Client) SignedUp(ctx context.Context, e busevents.SignedUp) (Status, error) {
	c.fill(&e.Meta, "signed-up-"+e.PersonID)
	return c.post(ctx, "/v1/events/"+busevents.AccountSignedUp, e)
}

// Activated reports that the person reached the app's first value; once per
// person, as its default EventID makes it.
func (c *Client) Activated(ctx context.Context, e busevents.Activated) (Status, error) {
	c.fill(&e.Meta, "activated-"+e.PersonID)
	return c.post(ctx, "/v1/events/"+busevents.AccountActivated, e)
}

// SubscriptionActivated, SubscriptionRenewed, SubscriptionPastDue and
// SubscriptionCanceled report the app's billing. AppSlug may stay empty: the
// app is the key's.
func (c *Client) SubscriptionActivated(ctx context.Context, e busevents.Subscription) (Status, error) {
	return c.subscription(ctx, busevents.SubscriptionActivated, e)
}

func (c *Client) SubscriptionRenewed(ctx context.Context, e busevents.Subscription) (Status, error) {
	return c.subscription(ctx, busevents.SubscriptionRenewed, e)
}

func (c *Client) SubscriptionPastDue(ctx context.Context, e busevents.Subscription) (Status, error) {
	return c.subscription(ctx, busevents.SubscriptionPastDue, e)
}

func (c *Client) SubscriptionCanceled(ctx context.Context, e busevents.Subscription) (Status, error) {
	return c.subscription(ctx, busevents.SubscriptionCanceled, e)
}

func (c *Client) subscription(ctx context.Context, name string, e busevents.Subscription) (Status, error) {
	c.fill(&e.Meta, "")
	return c.post(ctx, "/v1/events/"+name, e)
}

// DailyVisitors reports the app's unique visitors of one UTC day, for an app
// that counts them itself rather than with nakoda's browser SDK. A later count
// for the same day replaces the earlier one.
func (c *Client) DailyVisitors(ctx context.Context, e busevents.DailyVisitors) (Status, error) {
	c.fill(&e.Meta, "visitors-"+e.Date)
	return c.post(ctx, "/v1/visitors", e)
}

// Forget erases a person from the app in nakoda; events about them sent
// afterwards are not collected.
func (c *Client) Forget(ctx context.Context, personID string) (Status, error) {
	return c.send(ctx, http.MethodDelete, "/v1/people/"+url.PathEscape(personID), nil)
}

func (c *Client) fill(m *busevents.Meta, id string) {
	if m.EventID == "" {
		m.EventID = id
	}
	if m.EventID == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		m.EventID = hex.EncodeToString(b)
	}
	if m.OccurredAt.IsZero() {
		m.OccurredAt = c.now()
	}
}

func (c *Client) post(ctx context.Context, path string, e any) (Status, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("nakoda: %w", err)
	}
	return c.send(ctx, http.MethodPost, path, body)
}

// send retries what nakoda could not answer, with the same body and so the
// same event id: a retry of an event that did arrive is a duplicate, not a
// second event.
func (c *Client) send(ctx context.Context, method, path string, body []byte) (Status, error) {
	var last error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			if err := c.wait(ctx, time.Duration(200<<(attempt-1))*time.Millisecond); err != nil {
				return "", err
			}
		}
		status, err := c.once(ctx, method, path, body)
		if err == nil {
			return status, nil
		}
		last = err
		var api *APIError
		if errors.As(err, &api) && !api.Temporary() {
			return "", err
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", last
}

func (c *Client) once(ctx context.Context, method, path string, body []byte) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("nakoda: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("nakoda: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	var answer struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(raw, &answer)
	if res.StatusCode/100 != 2 {
		return "", &APIError{Status: res.StatusCode, Reason: answer.Error}
	}
	return Status(answer.Status), nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
