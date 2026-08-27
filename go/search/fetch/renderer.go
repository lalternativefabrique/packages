package fetch

import (
	"context"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
)

// minUsableRunes is the floor below which FetchWithFallback treats a static
// fetch as empty and retries through a Renderer. A JS-only page's initial
// HTML often still carries a shell (nav, footer boilerplate) that readability
// extracts a few dozen runes from, which is not an article.
const minUsableRunes = 200

// Renderer renders url's JavaScript and returns the resulting HTML. It is
// satisfied by rendersvc.Client, or by nothing at all — a caller that never
// wants JS rendering just does not wire one in, and FetchWithFallback
// degrades to FetchStatic's own result.
type Renderer interface {
	Render(ctx context.Context, url string, timeout time.Duration) (html string, err error)
}

// FetchWithFallback tries FetchStatic first. If the result has fewer than
// minUsableRunes of text and r is non-nil, it retries by rendering the page's
// JavaScript through r and re-running extraction on the rendered HTML.
//
// cache is consulted and populated the same way as in FetchStatic, for both
// the static attempt and — keyed separately, since it holds a different
// extraction of the same URL — a successful render.
func FetchWithFallback(ctx context.Context, rawURL string, r Renderer, maxRunes int, cache Cache) (*Page, error) {
	page, err := FetchStatic(ctx, rawURL, maxRunes, cache)
	if err != nil {
		if r == nil {
			return nil, err
		}
	} else if len([]rune(page.Text)) >= minUsableRunes {
		return page, nil
	}
	if r == nil {
		return page, nil
	}

	parsed, perr := url.Parse(rawURL)
	if perr != nil {
		if err != nil {
			return nil, err
		}
		return page, nil
	}

	renderedKey := rawURL + "#rendered"
	if cache != nil {
		if cached, ok := cache.Get(renderedKey); ok {
			return &Page{Title: cached.Title, Text: truncateRunes(cached.Text, maxRunes)}, nil
		}
	}

	html, rerr := r.Render(ctx, rawURL, 12*time.Second)
	if rerr != nil {
		if err != nil {
			return nil, err
		}
		return page, nil
	}

	title, text := extractHTML(html, parsed)
	if strings.TrimSpace(text) == "" {
		if err != nil {
			return nil, err
		}
		return page, nil
	}

	full := &Page{Title: title, Text: text}
	if cache != nil {
		cache.Set(renderedKey, full)
	}
	return &Page{Title: title, Text: truncateRunes(text, maxRunes)}, nil
}

func extractHTML(html string, parsed *url.URL) (title, text string) {
	defer func() {
		if recover() != nil {
			title, text = "", ""
		}
	}()

	article, err := readability.FromReader(strings.NewReader(html), parsed)
	if err != nil {
		return "", ""
	}
	var buf strings.Builder
	if err := article.RenderText(&buf); err != nil {
		return "", ""
	}
	return strings.TrimSpace(article.Title()), strings.TrimSpace(buf.String())
}
