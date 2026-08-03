package outbox

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lalternative/packages/go/webhooks/domain/providers"
)

func TestDispatchNamesHeadersAfterBrand(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(2*time.Second, "Lungor")
	d.client.Transport = nil

	res := d.Dispatch(context.Background(), providers.DeliveryJob{
		URL:           srv.URL,
		Secret:        "whsec_test",
		EventType:     "subscription.renewed",
		DeliveryID:    "d-1",
		SourceEventID: "e-1",
		Payload:       []byte(`{"a":1}`),
	})
	if res.Err != nil {
		t.Fatalf("dispatch: %v", res.Err)
	}

	if got.Get("Lungor-Signature") == "" {
		t.Fatal("expected Lungor-Signature header")
	}
	if got.Get("Spore-Signature") != "" {
		t.Fatal("brand leaked: Spore-Signature present on a Lungor dispatcher")
	}
	for _, h := range []string{"Lungor-Timestamp", "Lungor-Event", "Lungor-Delivery-ID", "Lungor-Source-Event-ID"} {
		if got.Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
	if ua := got.Get("User-Agent"); !strings.HasPrefix(ua, "Lungor-Webhooks/") {
		t.Errorf("user agent = %q, want Lungor-Webhooks/ prefix", ua)
	}
}

// The signature must not depend on the brand: a subscriber verifying with the
// documented scheme has to succeed whichever product sent the event.
func TestSignatureIsBrandIndependent(t *testing.T) {
	const secret, ts = "whsec_test", "1700000000"
	body := []byte(`{"a":1}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	if got := sign(secret, ts, body); got != want {
		t.Fatalf("sign() = %s, want %s", got, want)
	}
}

func TestDefaultBrandWhenUnset(t *testing.T) {
	if d := NewHTTPDispatcher(0, ""); d.brand != DefaultBrand {
		t.Fatalf("brand = %q, want %q", d.brand, DefaultBrand)
	}
}
