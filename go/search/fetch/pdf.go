package fetch

import (
	"bytes"
	"context"
	"io"
	"log"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	// maxPDFBytes bounds a PDF written to disk for extraction: a paper is a
	// few megabytes where a scanned book is not.
	maxPDFBytes = 32 << 20
	pdfTimeout  = 30 * time.Second
)

var warnNoPoppler sync.Once

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
// Without the binaries the page comes back empty, and the log says why.
//
// Markdown carries the same text: a PDF's layout does not survive
// extraction, and a caller asking for markdown still wants the content.
func extractPDF(ctx context.Context, body io.Reader, page *url.URL) *Page {
	out := &Page{URL: page.String(), Title: strings.TrimSuffix(path.Base(page.Path), ".pdf")}

	if _, err := exec.LookPath("pdftotext"); err != nil {
		warnNoPoppler.Do(func() { log.Print("fetch: pdftotext not installed, PDFs read as empty pages") })
		return out
	}
	file, err := os.CreateTemp("", "fetch-*.pdf")
	if err != nil {
		return out
	}
	defer os.Remove(file.Name())
	_, err = io.Copy(file, io.LimitReader(body, maxPDFBytes))
	file.Close()
	if err != nil {
		return out
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
		return out
	}
	out.Text = strings.TrimSpace(strings.ReplaceAll(string(text), "\f", "\n\n"))
	out.Markdown = out.Text
	return out
}

func pdfInfoTitle(info []byte) string {
	for _, line := range bytes.Split(info, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("Title:")); ok {
			return strings.TrimSpace(string(rest))
		}
	}
	return ""
}
