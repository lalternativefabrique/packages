package sdk

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lalternative/packages/vvaves/sdk-go/signed"
)

func TestNotConfiguredWithoutBaseURL(t *testing.T) {
	c := New("", "key")
	if _, err := c.Speak(context.Background(), Reading{Text: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Speak = %v, want ErrNotConfigured", err)
	}
	if _, err := c.Sign(context.Background(), Reading{Text: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Sign = %v, want ErrNotConfigured", err)
	}
}

func TestKeyTravelsOnTheHeaderItsKindNeeds(t *testing.T) {
	for key, want := range map[string][2]string{
		"app-key":            {HeaderKey, "app-key"},
		"vvaves_key_abc.sig": {"Authorization", "Bearer vvaves_key_abc.sig"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get(want[0]); got != want[1] {
				t.Errorf("%s: %s = %q, want %q", key, want[0], got, want[1])
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ready":true}`))
		}))
		ready, err := New(srv.URL, key).Exists(context.Background(), Reading{Text: "x", ID: "1"})
		if err != nil || !ready {
			t.Errorf("%s: Exists = %v, %v", key, ready, err)
		}
		srv.Close()
	}
}

func TestSpeakSendsTheReadingWithTheClientScope(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("mp3"))
	}))
	defer srv.Close()
	a, err := New(srv.URL, "k", WithScope("synthiz")).Speak(context.Background(), Reading{Text: "hi", ID: "7", Gender: GenderMale})
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Bytes) != "mp3" || a.MIME != "audio/mpeg" {
		t.Errorf("audio = %q %s", a.Bytes, a.MIME)
	}
	if got["scope"] != "synthiz" || got["id"] != "7" || got["gender"] != "male" || got["stream"] != nil {
		t.Errorf("body = %v", got)
	}
}

func frames(pieces ...[]byte) []byte {
	var out []byte
	for _, p := range pieces {
		out = binary.BigEndian.AppendUint32(out, uint32(len(p)))
		out = append(out, p...)
	}
	return out
}

func TestSpeakStreamEmitsEachFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", FramesContentType)
		w.Write(frames([]byte("a"), nil, []byte("bc")))
	}))
	defer srv.Close()
	var got []string
	_, err := New(srv.URL, "k").SpeakStream(context.Background(), Reading{Text: "x"}, func(b []byte) error {
		got = append(got, string(b))
		return nil
	})
	if err != nil || strings.Join(got, ",") != "a,bc" {
		t.Errorf("frames = %v, err = %v", got, err)
	}
}

func TestSpeakStreamRefusesAnOversizedFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", FramesContentType)
		w.Write(binary.BigEndian.AppendUint32(nil, MaxFrameBytes+1))
	}))
	defer srv.Close()
	_, err := New(srv.URL, "k").SpeakStream(context.Background(), Reading{Text: "x"}, func([]byte) error { return nil })
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestStatusMapsOntoSentinels(t *testing.T) {
	for code, want := range map[int]error{400: ErrBadRequest, 401: ErrUnauthorized, 403: ErrUnauthorized, 503: ErrUnavailable} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			w.Write([]byte(`{"error":"nope"}`))
		}))
		if _, err := New(srv.URL, "k").Speak(context.Background(), Reading{Text: "x"}); !errors.Is(err, want) {
			t.Errorf("%d: err = %v, want %v", code, err, want)
		}
		srv.Close()
	}
}

func TestLocalSignatureVerifiesOnTheServerScheme(t *testing.T) {
	c := New("http://vvaves.internal", "app-key", WithScope("s"), WithSigning("synthiz", "https://vvaves.example.com", time.Minute))
	su, err := c.Sign(context.Background(), Reading{Text: "hello", ID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(su.URL)
	if u.Host != "vvaves.example.com" || u.Path != "/speak" {
		t.Errorf("url = %s", su.URL)
	}
	v := signed.NewLookupVerifier(func(iss string) []string {
		if iss == "synthiz" {
			return []string{"app-key"}
		}
		return nil
	})
	if err := v.Verify(u.Query(), "s", "1", "hello"); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if c.PublicOrigin() != "https://vvaves.example.com" {
		t.Errorf("PublicOrigin = %s", c.PublicOrigin())
	}
}

func TestSignRefusesFieldsTheSchemeCannotSeparate(t *testing.T) {
	c := New("http://vvaves.internal", "app-key", WithSigning("synthiz", "", 0))
	if _, err := c.Sign(context.Background(), Reading{Text: "x", ID: "a\nscope=b"}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("err = %v, want ErrBadRequest", err)
	}
	if _, err := New("http://vvaves.internal", "app-key").Sign(context.Background(), Reading{Text: "x"}); !errors.Is(err, ErrNoIssuer) {
		t.Errorf("no issuer: err = %v, want ErrNoIssuer", err)
	}
}

func TestCustomerKeyIsSignedByVvaves(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speak/sign" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"url":"https://vvaves.example.com/speak?iss=vvaves&sig=x","expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	su, err := New(srv.URL, "vvaves_key_abc").Sign(context.Background(), Reading{Text: "x"})
	if err != nil || !strings.Contains(su.URL, "iss=vvaves") || su.ExpiresAt.Year() != 2030 {
		t.Errorf("Sign = %+v, %v", su, err)
	}
}

func TestCustomerKeySignatureOverPlainHTTPIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"url":"http://vvaves.example.com/speak?sig=x","expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "vvaves_key_abc").Sign(context.Background(), Reading{Text: "x"}); !errors.Is(err, ErrInsecureBaseURL) {
		t.Errorf("err = %v, want ErrInsecureBaseURL", err)
	}
}
