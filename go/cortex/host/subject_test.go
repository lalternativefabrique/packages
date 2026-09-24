package host

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTwoPeopleSharingAContextIDDoNotShareItsHistory(t *testing.T) {
	provider, bodies := fakeModel(t, "")
	srv := start(t, Config{Agent: Agent{Name: "a"}, Provider: provider, Token: "t"})

	readStream(t, rpc(t, srv.URL, "t", "message/stream", message("le budget secret de marie", "ctx-same", map[string]any{SubjectKey: "marie"})))
	readStream(t, rpc(t, srv.URL, "t", "message/stream", message("bonjour", "ctx-same", map[string]any{SubjectKey: "paul"})))

	if last := (*bodies)[len(*bodies)-1]; strings.Contains(last, "secret de marie") {
		t.Fatalf("paul's turn carried marie's history: %s", last)
	}
}

func signSubject(t *testing.T, key ed25519.PrivateKey, subject, audience string, expires time.Time) string {
	t.Helper()
	raw, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Subject: subject, Audience: jwt.ClaimStrings{audience}, ExpiresAt: jwt.NewNumericDate(expires),
	}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func finalState(t *testing.T, events []event) string {
	t.Helper()
	for _, ev := range events {
		if ev.Kind == "status-update" && ev.Final {
			return ev.Status.State
		}
	}
	t.Fatalf("no final status in %+v", events)
	return ""
}

func TestAHostHoldingASubjectKeyActsOnlyForTheProvenSubject(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	_, forger, _ := ed25519.GenerateKey(rand.Reader)
	hour := time.Now().Add(time.Hour)

	cases := []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"signed by another key", signSubject(t, forger, "marie", "cerveau", hour)},
		{"for another agent", signSubject(t, private, "marie", "other", hour)},
		{"expired", signSubject(t, private, "marie", "cerveau", time.Now().Add(-time.Minute))},
		{"no subject", signSubject(t, private, "", "cerveau", hour)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider, bodies := fakeModel(t, "")
			srv := start(t, Config{Agent: Agent{Name: "cerveau"}, Provider: provider, Token: "t", Recall: &memory{}, SubjectKey: public})
			meta := map[string]any{SubjectKey: "marie"}
			if c.token != "" {
				meta[SubjectTokenKey] = c.token
			}

			state := finalState(t, readStream(t, rpc(t, srv.URL, "t", "message/stream", message("quel budget ?", "ctx-1", meta))))

			if state != "rejected" || len(*bodies) != 0 {
				t.Fatalf("state %q after %d model calls, want rejected before any", state, len(*bodies))
			}
		})
	}
}

func TestAProvenSubjectWinsOverTheDeclaredOne(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	provider, _ := fakeModel(t, "")
	mem := &memory{}
	srv := start(t, Config{Agent: Agent{Name: "cerveau"}, Provider: provider, Token: "t", Recall: mem, SubjectKey: public})

	state := finalState(t, readStream(t, rpc(t, srv.URL, "t", "message/stream", message("quel budget ?", "ctx-1", map[string]any{
		SubjectKey:      "paul",
		SubjectTokenKey: signSubject(t, private, "marie", "cerveau", time.Now().Add(time.Hour)),
	}))))

	if state != "completed" || len(mem.entries) == 0 {
		t.Fatalf("state %q, %d entries", state, len(mem.entries))
	}
	for _, e := range mem.entries {
		if e.Subject != "marie" {
			t.Fatalf("remembered under %q, want the proven marie", e.Subject)
		}
	}
}
