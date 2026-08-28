package infrastructure

import (
	"context"
	"fmt"
	"html"
	"strings"
)

// EmailSender is the minimal mail-sending capability the host app supplies.
// Kept as a narrow interface here rather than importing the host's own mail
// package, so this module has no opinion on how mail actually leaves the
// building.
type EmailSender interface {
	Send(ctx context.Context, to, subject, htmlBody string) error
}

// EmailChannel delivers a reminder by email. Target is the recipient address.
type EmailChannel struct {
	sender EmailSender
}

func NewEmailChannel(sender EmailSender) *EmailChannel {
	return &EmailChannel{sender: sender}
}

func (c *EmailChannel) Type() string { return "email" }

// Send renders body as HTML: the first line is the summary, the rest are
// bullet points.
func (c *EmailChannel) Send(ctx context.Context, title, body, target string) error {
	return c.sender.Send(ctx, target, title, renderHTML(title, body))
}

func renderHTML(title, body string) string {
	lines := strings.Split(body, "\n")
	summary := ""
	if len(lines) > 0 {
		summary = lines[0]
		lines = lines[1:]
	}
	var details strings.Builder
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		details.WriteString("<li>")
		details.WriteString(html.EscapeString(l))
		details.WriteString("</li>")
	}
	return fmt.Sprintf(
		`<div style="font-family:system-ui,sans-serif;line-height:1.5;color:#1f2937">`+
			`<h2 style="margin:0 0 8px 0;font-size:18px">%s</h2>`+
			`<p style="margin:0 0 12px 0">%s</p>`+
			`<ul style="padding-left:18px;margin:0 0 12px 0;font-size:13px;color:#4b5563">%s</ul>`+
			`</div>`,
		html.EscapeString(title),
		html.EscapeString(summary),
		details.String(),
	)
}
