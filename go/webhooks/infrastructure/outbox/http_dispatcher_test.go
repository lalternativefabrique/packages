package outbox

import (
	"context"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/webhooks/domain/providers"
)

func TestHTTPDispatcherRejectsPrivateTargets(t *testing.T) {
	d := NewHTTPDispatcher(0, "Test")

	targets := []string{
		"http://127.0.0.1:1/webhook",
		"http://10.0.0.1/webhook",
		"http://169.254.169.254/latest/meta-data/",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			res := d.Dispatch(context.Background(), providers.DeliveryJob{
				URL:       target,
				Secret:    "whsec_test",
				Payload:   []byte(`{"ok":true}`),
				EventType: "email.sent",
			})
			if res.Err == nil {
				t.Fatal("expected private target to be rejected")
			}
			if !strings.Contains(res.Err.Error(), "private address") {
				t.Fatalf("expected private-address error, got %v", res.Err)
			}
		})
	}
}
