package authz_test

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/authz"
	"github.com/lalternative/packages/go/svcauth"
)

//go:embed testdata/*.rego
var policies embed.FS

func pdp(t *testing.T) authz.PDP {
	t.Helper()
	sub, err := fs.Sub(policies, "testdata")
	if err != nil {
		t.Fatal(err)
	}
	p, err := authz.OPA(sub, "data.test.cortex")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSubjectFromClaims(t *testing.T) {
	s := authz.SubjectFromClaims(svcauth.Claims{ClientID: "ak_1", Owner: "id-42", Roles: []string{"lalter:user"}, Scopes: []string{"lalter:chat"}})
	if s.Type() != "user" || s.ID() != "id-42" || s.ClientID != "ak_1" {
		t.Fatalf("subject = %+v", s)
	}
	m := authz.SubjectFromClaims(svcauth.Claims{ClientID: "lalter-core"})
	if m.Type() != "machine" || m.ID() != "lalter-core" {
		t.Fatalf("subject = %+v", m)
	}
}

func TestOPADecisions(t *testing.T) {
	p := pdp(t)
	user := authz.Subject{Owner: "id-42", ClientID: "ak_1", Roles: []string{"lalter:user"}}
	cases := []struct {
		name   string
		req    authz.Request
		allow  bool
		reason string
	}{
		{"readonly bash", authz.Request{Subject: user, Action: authz.Action{Name: "bash", Properties: map[string]any{"command": "ls"}}}, true, ""},
		{"other bash", authz.Request{Subject: user, Action: authz.Action{Name: "bash", Properties: map[string]any{"command": "rm -rf"}}}, false, authz.ReasonDeniedByPolicy},
		{"write in workspace", authz.Request{Subject: user, Action: authz.Action{Name: "write"}, Resource: authz.Resource{Type: "lalter:file", ID: "/ws/a.go"}, Context: map[string]any{"workspace": "/ws"}}, true, ""},
		{"write outside", authz.Request{Subject: user, Action: authz.Action{Name: "write"}, Resource: authz.Resource{Type: "lalter:file", ID: "/etc/passwd"}, Context: map[string]any{"workspace": "/ws"}}, false, authz.ReasonDeniedByPolicy},
		{"delegate unapproved", authz.Request{Subject: user, Action: authz.Action{Name: "delegate"}}, false, authz.ReasonApprovalRequired},
		{"delegate approved", authz.Request{Subject: user, Action: authz.Action{Name: "delegate"}, Context: map[string]any{"approval": map[string]any{"granted": true}}}, true, ""},
		{"role read", authz.Request{Subject: user, Action: authz.Action{Name: "read"}}, true, ""},
		{"machine read", authz.Request{Subject: authz.Subject{ClientID: "lalter-core"}, Action: authz.Action{Name: "read"}}, false, authz.ReasonDeniedByPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := p.Evaluate(context.Background(), tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if d.Allow != tc.allow || d.Reason != tc.reason {
				t.Fatalf("decision = %+v, want allow=%v reason=%q", d, tc.allow, tc.reason)
			}
		})
	}
	d, _ := p.Evaluate(context.Background(), authz.Request{Subject: user, Action: authz.Action{Name: "write"}, Resource: authz.Resource{ID: "/ws/x"}, Context: map[string]any{"workspace": "/ws"}})
	if d.Context["workspace"] != "/ws" {
		t.Fatalf("context = %+v", d.Context)
	}
}

func TestOPARefusesMissingOrBrokenPolicies(t *testing.T) {
	if _, err := authz.OPA(fstest_(map[string]string{}), "data.x"); err == nil {
		t.Fatal("expected an error without policies")
	}
	if _, err := authz.OPA(fstest_(map[string]string{"a.rego": "package x\nallow if {"}), "data.x"); err == nil {
		t.Fatal("expected a compile error")
	}
}

func TestHTTPRoundTrip(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		authz.Handler(pdp(t)).ServeHTTP(w, r)
	}))
	defer srv.Close()
	client := authz.HTTP(srv.URL, "s3cret", srv.Client())
	d, err := client.Evaluate(context.Background(), authz.Request{
		Subject: authz.Subject{Owner: "id-42", Roles: []string{"lalter:user"}},
		Action:  authz.Action{Name: "delegate"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allow || d.Reason != authz.ReasonApprovalRequired || seenAuth != "Bearer s3cret" {
		t.Fatalf("decision = %+v, auth = %q", d, seenAuth)
	}
	d, err = client.Evaluate(context.Background(), authz.Request{
		Subject:  authz.Subject{Owner: "id-42", Roles: []string{"lalter:user"}},
		Action:   authz.Action{Name: "write"},
		Resource: authz.Resource{Type: "lalter:file", ID: "/ws/a"},
		Context:  map[string]any{"workspace": "/ws"},
	})
	if err != nil || !d.Allow || d.Context["workspace"] != "/ws" {
		t.Fatalf("decision = %+v, err = %v", d, err)
	}
}

func TestHTTPFailureIsUnavailableNotDenial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := authz.HTTP(srv.URL, "", srv.Client()).Evaluate(context.Background(), authz.Request{})
	if !errors.Is(err, authz.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	srv.Close()
	_, err = authz.HTTP(srv.URL, "", srv.Client()).Evaluate(context.Background(), authz.Request{})
	if !errors.Is(err, authz.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandlerRejectsBadRequests(t *testing.T) {
	h := authz.Handler(authz.AllowAll())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, authz.EvaluationPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, authz.EvaluationPath, strings.NewReader("{")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
}

func TestStubsAndLogging(t *testing.T) {
	var lines strings.Builder
	logger := slog.New(slog.NewTextHandler(&lines, nil))
	d, err := authz.Logged(authz.DenyAll(), authz.Slog(logger), authz.SpanEvents()).
		Evaluate(context.Background(), authz.Request{Subject: authz.Subject{ClientID: "c"}, Action: authz.Action{Name: "send"}})
	if err != nil || d.Allow || d.Reason != authz.ReasonDeniedByPolicy {
		t.Fatalf("decision = %+v, err = %v", d, err)
	}
	if !strings.Contains(lines.String(), "action=send") || !strings.Contains(lines.String(), "allow=false") {
		t.Fatalf("log = %q", lines.String())
	}
	d, _ = authz.AllowAll().Evaluate(context.Background(), authz.Request{})
	if !d.Allow {
		t.Fatal("AllowAll denied")
	}
}
