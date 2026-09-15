package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

const (
	// maxPDFBytes bounds a PDF written to disk for extraction: a paper is a
	// few megabytes where a scanned book is not.
	maxPDFBytes = 32 << 20
	pdfTimeout  = 30 * time.Second
)

// ErrNoPoppler reports a host without pdftotext: a PDF cannot be read
// there, and saying so beats an empty page that looks like the document.
var ErrNoPoppler = errors.New("fetch page: pdftotext is not installed")

func isPDF(contentType string, page *url.URL) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "application/pdf" {
		return true
	}
	byName := strings.HasSuffix(strings.ToLower(page.Path), ".pdf")
	return byName && (mediaType == "" || mediaType == "application/octet-stream")
}

// extractPDF reads the document with poppler's pdftotext and pdfinfo. A
// pure-Go reader was tried first and took minutes on an ordinary arXiv
// paper; poppler takes a tenth of a second and gets reading order right.
//
// A download cut short or a file pdftotext cannot read is an error, not a
// page: an empty page would be cached and served for the cache's whole
// TTL, which is what happened when a caller gave up on a slow download.
//
// Markdown carries the same text: a PDF's layout does not survive
// extraction, and a caller asking for markdown still wants the content.
func extractPDF(ctx context.Context, body io.Reader, page *url.URL) (*Page, error) {
	out := &Page{URL: page.String(), Title: strings.TrimSuffix(path.Base(page.Path), ".pdf")}

	if _, err := exec.LookPath("pdftotext"); err != nil {
		return nil, ErrNoPoppler
	}
	file, err := os.CreateTemp("", "fetch-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}
	defer os.Remove(file.Name())
	_, err = io.Copy(file, io.LimitReader(body, maxPDFBytes))
	file.Close()
	if err != nil {
		return nil, fmt.Errorf("fetch page: download: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, pdfTimeout)
	defer cancel()

	if info, err := exec.CommandContext(ctx, "pdfinfo", file.Name()).Output(); err == nil {
		if title := pdfInfoTitle(info); title != "" {
			out.Title = title
		}
	}
	text, err := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", file.Name(), "-").Output()
	if err != nil {
		return nil, fmt.Errorf("fetch page: pdftotext: %w", err)
	}
	out.Text = strings.TrimSpace(strings.ReplaceAll(string(text), "\f", "\n\n"))
	out.Markdown = out.Text
	return out, nil
}

func pdfInfoTitle(info []byte) string {
	for _, line := range bytes.Split(info, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("Title:")); ok {
			return strings.TrimSpace(string(rest))
		}
	}
	return ""
}
