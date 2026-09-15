package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func servePDF(t *testing.T, contentType, filePath string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile("testdata/paper.pdf")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != filePath {
			http.NotFound(w, r)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchStaticReadsAPDFByContentType(t *testing.T) {
	srv := servePDF(t, "application/pdf", "/download")

	page, err := FetchStatic(context.Background(), srv.URL+"/download", 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if page.Title != "Attention Is All You Need" {
		t.Errorf("Title = %q, want the PDF's metadata title", page.Title)
	}
	for _, want := range []string{"Abstract: The dominant sequence", "2 Background"} {
		if !strings.Contains(page.Text, want) {
			t.Errorf("Text lacks %q:\n%s", want, page.Text)
		}
	}
	if page.Markdown != page.Text {
		t.Errorf("Markdown should carry the PDF's text")
	}
	if len(page.Links) != 0 {
		t.Errorf("a PDF has no links to list, got %v", page.Links)
	}
}

func TestFetchStaticReadsAPDFByNameWhenTheTypeIsGeneric(t *testing.T) {
	srv := servePDF(t, "application/octet-stream", "/papers/1706.03762.pdf")

	page, err := FetchStatic(context.Background(), srv.URL+"/papers/1706.03762.pdf", 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if !strings.Contains(page.Text, "Abstract") {
		t.Errorf("Text = %q, want the PDF's text", page.Text)
	}
}

func TestFetchStaticTruncatesAPDF(t *testing.T) {
	srv := servePDF(t, "application/pdf", "/p.pdf")

	page, err := FetchStatic(context.Background(), srv.URL+"/p.pdf", 40, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if len([]rune(page.Text)) > 41 {
		t.Errorf("Text not truncated: %q", page.Text)
	}
}
