package fetch

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRenderer struct {
	called bool
	html   string
	err    error
}

func (f *fakeRenderer) Render(ctx context.Context, url string, timeout time.Duration) (string, error) {
	f.called = true
	return f.html, f.err
}

func TestFetchWithFallbackSkipsRendererOnUsableStaticText(t *testing.T) {
	srv := serveHTML(t, articleHTML)
	defer srv.Close()

	r := &fakeRenderer{}
	if _, err := FetchWithFallback(context.Background(), srv.URL, r, 6000, nil); err != nil {
		t.Fatalf("FetchWithFallback: %v", err)
	}
	if r.called {
		t.Error("Renderer was called although the static fetch already yielded usable text")
	}
}

func TestFetchWithFallbackUsesRendererOnEmptyStaticText(t *testing.T) {
	srv := serveHTML(t, jsOnlyShellHTML)
	defer srv.Close()

	r := &fakeRenderer{html: articleHTML}
	page, err := FetchWithFallback(context.Background(), srv.URL, r, 6000, nil)
	if err != nil {
		t.Fatalf("FetchWithFallback: %v", err)
	}
	if !r.called {
		t.Error("Renderer was not called although the static fetch yielded no usable text")
	}
	if page.Text == "" {
		t.Error("Text is empty, want the rendered page's extracted text")
	}
}

func TestFetchWithFallbackWithoutRendererReturnsStaticResult(t *testing.T) {
	srv := serveHTML(t, jsOnlyShellHTML)
	defer srv.Close()

	page, err := FetchWithFallback(context.Background(), srv.URL, nil, 6000, nil)
	if err != nil {
		t.Fatalf("FetchWithFallback: %v", err)
	}
	if page == nil {
		t.Fatal("page is nil, want the (possibly empty) static result")
	}
}

func TestFetchWithFallbackFallsBackToStaticOnRendererError(t *testing.T) {
	srv := serveHTML(t, jsOnlyShellHTML)
	defer srv.Close()

	r := &fakeRenderer{err: errors.New("render service down")}
	page, err := FetchWithFallback(context.Background(), srv.URL, r, 6000, nil)
	if err != nil {
		t.Fatalf("FetchWithFallback: %v", err)
	}
	if page == nil {
		t.Fatal("page is nil, want the static result even though rendering failed")
	}
}
