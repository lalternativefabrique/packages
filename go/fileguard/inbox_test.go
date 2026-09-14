package fileguard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBucket struct {
	key         string
	contentType string
	body        []byte
	err         error
}

func (f *fakeBucket) Upload(_ context.Context, key string, body io.Reader, contentType string) error {
	f.key, f.contentType = key, contentType
	b, err := io.ReadAll(body)
	f.body = b
	if err != nil {
		return err
	}
	return f.err
}

func mp3Body(total int) []byte {
	head := []byte("ID3\x04\x00\x00\x00\x00\x00\x00")
	return append(head, bytes.Repeat([]byte("x"), total-len(head))...)
}

func serving(body []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
}

// The whole point of the deposit: the bytes reach the bucket intact, including
// the header the sniffer consumed. Losing those 512 bytes would corrupt every
// file while leaving the transfer looking successful.
func TestDepositReader_StoresEveryByteIncludingTheSniffedHeader(t *testing.T) {
	want := mp3Body(4096)
	bucket := &fakeBucket{}

	got, err := DepositReader(context.Background(), bucket, "techtuel/job-1", bytes.NewReader(want),
		DepositOptions{MaxBytes: 1 << 20, RequireMedia: true})
	if err != nil {
		t.Fatalf("DepositReader: %v", err)
	}
	if !bytes.Equal(bucket.body, want) {
		t.Errorf("stored %d bytes, want %d", len(bucket.body), len(want))
	}
	if bucket.key != "techtuel/job-1" {
		t.Errorf("key = %q", bucket.key)
	}
	// The content type comes from the bytes, never from what anyone declared.
	if bucket.contentType != "audio/mpeg" {
		t.Errorf("contentType = %q, want audio/mpeg", bucket.contentType)
	}
	if got.Bytes != int64(len(want)) {
		t.Errorf("Bytes = %d, want %d", got.Bytes, len(want))
	}
	if got.Container.MIME != "audio/mpeg" {
		t.Errorf("Container.MIME = %q", got.Container.MIME)
	}
}

// Nothing reaches the bucket when the bytes are not what the pipeline handles:
// storing first and judging later leaves hostile content at rest.
func TestDepositReader_RejectsBeforeStoring(t *testing.T) {
	cases := map[string][]byte{
		"not a container":    []byte("<!DOCTYPE html><html><body>404</body></html>"),
		"document not media": []byte("%PDF-1.7\n%\xc7\xec"),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			bucket := &fakeBucket{}
			_, err := DepositReader(context.Background(), bucket, "k", bytes.NewReader(body),
				DepositOptions{MaxBytes: 1 << 20, RequireMedia: true})
			if err == nil {
				t.Fatal("deposit accepted")
			}
			if bucket.body != nil {
				t.Errorf("%d bytes reached the bucket despite the refusal", len(bucket.body))
			}
		})
	}
}

// A PDF is a container Sniff recognises, so only RequireMedia separates the
// media pipeline from a document one.
func TestDepositReader_RequireMediaIsWhatRefusesADocument(t *testing.T) {
	pdf := []byte("%PDF-1.7\n%\xc7\xec" + strings.Repeat("x", 100))

	if _, err := DepositReader(context.Background(), &fakeBucket{}, "k", bytes.NewReader(pdf),
		DepositOptions{MaxBytes: 1 << 20, RequireMedia: true}); !errors.Is(err, ErrNotMedia) {
		t.Errorf("with RequireMedia: error = %v, want ErrNotMedia", err)
	}

	bucket := &fakeBucket{}
	if _, err := DepositReader(context.Background(), bucket, "k", bytes.NewReader(pdf),
		DepositOptions{MaxBytes: 1 << 20}); err != nil {
		t.Errorf("without RequireMedia: %v", err)
	}
	if bucket.contentType != "application/pdf" {
		t.Errorf("contentType = %q, want application/pdf", bucket.contentType)
	}
}

// The ceiling applies to bytes that arrive. A source can claim any length, or
// none at all, so a limit that trusts a declaration is decoration.
func TestDepositReader_CeilingIsOnArrivedBytes(t *testing.T) {
	_, err := DepositReader(context.Background(), &fakeBucket{}, "k", bytes.NewReader(mp3Body(8192)),
		DepositOptions{MaxBytes: 2048, RequireMedia: true})
	if !errors.Is(err, ErrDepositTooLarge) {
		t.Fatalf("error = %v, want ErrDepositTooLarge", err)
	}
}

func TestDepositFromURL_FetchesAndStores(t *testing.T) {
	want := mp3Body(2048)
	srv := serving(want)
	defer srv.Close()

	// The deposit fetches through SafeHTTPClient, whose dialer refuses loopback:
	// that is the same guard that closes rebinding, and it fires here.
	if _, err := DepositFromURL(context.Background(), &fakeBucket{}, "k", srv.URL,
		DepositOptions{MaxBytes: 1 << 20, RequireMedia: true}); err == nil {
		t.Fatal("a loopback source was fetched")
	}
}

// A source the guard refuses never opens a connection, so nothing is stored.
func TestDepositFromURL_RefusesUnsafeURLBeforeFetching(t *testing.T) {
	bucket := &fakeBucket{}
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"",
	} {
		_, err := DepositFromURL(context.Background(), bucket, "k", u, DepositOptions{MaxBytes: 1 << 20})
		if !errors.Is(err, ErrUnsafeURL) {
			t.Errorf("DepositFromURL(%q) error = %v, want ErrUnsafeURL", u, err)
		}
	}
	if bucket.body != nil {
		t.Error("bytes were stored for a refused source")
	}
}

func TestDeposit_RequiresStorage(t *testing.T) {
	if _, err := DepositReader(context.Background(), nil, "k", bytes.NewReader(mp3Body(64)), DepositOptions{}); err == nil {
		t.Error("nil storage accepted")
	}
	if _, err := DepositFromURL(context.Background(), nil, "k", "https://1.1.1.1/a.mp3", DepositOptions{}); err == nil {
		t.Error("nil storage accepted")
	}
}
