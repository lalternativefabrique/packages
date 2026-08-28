package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SlackChannel delivers a reminder via Slack incoming webhook. Target is the
// webhook URL.
type SlackChannel struct {
	client *http.Client
}

func NewSlackChannel() *SlackChannel {
	return &SlackChannel{client: &http.Client{}}
}

func (c *SlackChannel) Type() string { return "slack" }

func (c *SlackChannel) Send(ctx context.Context, title, body, target string) error {
	payload := map[string]string{"text": fmt.Sprintf("*%s*\n%s", title, body)}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}
	return nil
}
