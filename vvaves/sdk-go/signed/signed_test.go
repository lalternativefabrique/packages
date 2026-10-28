package signed

import (
	"errors"
	"net/url"
	"testing"
	"time"
)

func verifier(key string) *Verifier {
	return NewLookupVerifier(func(iss string) []string {
		if iss == "app" {
			return []string{key}
		}
		return nil
	})
}

func TestRoundTrip(t *testing.T) {
	q, err := Sign("app", "k", Params{Scope: "s", ID: "1", TextHash: HashText("hello"), Expires: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier("k").Verify(q, "s", "1", "hello"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := verifier("k").Verify(q, "s", "1", "other text"); !errors.Is(err, ErrBadSignature) {
		t.Errorf("other text: %v, want ErrBadSignature", err)
	}
	if err := verifier("other").Verify(q, "s", "1", "hello"); !errors.Is(err, ErrBadSignature) {
		t.Errorf("other key: %v, want ErrBadSignature", err)
	}
}

func TestExpiredIsToldApart(t *testing.T) {
	q, _ := Sign("app", "k", Params{Scope: "s", ID: "1", TextHash: HashText("x"), Expires: time.Now().Add(-time.Second)})
	if err := verifier("k").Verify(q, "s", "1", "x"); !errors.Is(err, ErrExpired) {
		t.Errorf("err = %v, want ErrExpired", err)
	}
}

func TestControlCharactersAreRefused(t *testing.T) {
	for _, p := range []Params{{Scope: "a\nid=b"}, {ID: "x\x00"}, {ID: string(make([]byte, MaxFieldLen+1))}} {
		if _, err := Sign("app", "k", p); !errors.Is(err, ErrInvalidField) {
			t.Errorf("Sign(%q,%q) = %v, want ErrInvalidField", p.Scope, p.ID, err)
		}
	}
	q, _ := Sign("app", "k", Params{Scope: "s", TextHash: HashText("x"), Expires: time.Now().Add(time.Minute)})
	if err := verifier("k").Verify(q, "s\n", "", "x"); !errors.Is(err, ErrBadSignature) {
		t.Errorf("Verify with a newline = %v, want ErrBadSignature", err)
	}
}

// The canonical form must stay byte-identical to the one deployed vvaves
// servers verify, or every URL signed by this package is refused.
func TestCanonicalFormIsStable(t *testing.T) {
	got := sign(signingKey("k"), Params{Scope: "s", ID: "1", TextHash: HashText("hello"), Expires: time.Unix(1700000000, 0)})
	const want = "60Zb8gzc7UGApp-1aSTTaw9yU6f78enEiYz426cGUbk"
	if got != want {
		t.Errorf("signature = %s, want %s", got, want)
	}
}

func TestClientAndUserAreSigned(t *testing.T) {
	q, err := Sign("app", "k", Params{Scope: "s", ID: "1", TextHash: HashText("x"), Expires: time.Now().Add(time.Minute), Client: "ak_1", User: "u42"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Get(QueryClient) != "ak_1" || q.Get(QueryUser) != "u42" {
		t.Fatalf("query = %v", q)
	}
	if err := verifier("k").Verify(q, "s", "1", "x"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	for field, forged := range map[string]string{QueryUser: "u43", QueryClient: "ak_2"} {
		f := cloneValues(q)
		f.Set(field, forged)
		if err := verifier("k").Verify(f, "s", "1", "x"); !errors.Is(err, ErrBadSignature) {
			t.Errorf("forged %s: %v, want ErrBadSignature", field, err)
		}
		f.Del(field)
		if err := verifier("k").Verify(f, "s", "1", "x"); !errors.Is(err, ErrBadSignature) {
			t.Errorf("dropped %s: %v, want ErrBadSignature", field, err)
		}
	}
}

func TestUserCannotBeAddedToAnUnattributedURL(t *testing.T) {
	q, _ := Sign("app", "k", Params{Scope: "s", ID: "1", TextHash: HashText("x"), Expires: time.Now().Add(time.Minute)})
	q.Set(QueryUser, "u42")
	if err := verifier("k").Verify(q, "s", "1", "x"); !errors.Is(err, ErrBadSignature) {
		t.Errorf("added user: %v, want ErrBadSignature", err)
	}
}

func cloneValues(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}
