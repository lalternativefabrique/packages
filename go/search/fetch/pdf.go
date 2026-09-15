package fetch

import (
	"bytes"
	"io"
	"mime"
	"net/url"
	"path"
	"strings"

	"github.com/dslipak/pdf"
)

// maxPDFBytes bounds a PDF read into memory: the reader needs random
// access, and a paper is a few megabytes where a scanned book is not.
const maxPDFBytes = 32 << 20

func isPDF(contentType string, page *url.URL) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "application/pdf" {
		return true
	}
	byName := strings.HasSuffix(strings.ToLower(page.Path), ".pdf")
	return byName && (mediaType == "" || mediaType == "application/octet-stream")
}

// extractPDF reads the document's text page by page. Markdown carries the
// same text: a PDF's layout does not survive extraction, and a caller
// asking for markdown still wants the content rather than nothing.
func extractPDF(body io.Reader, page *url.URL) (out *Page) {
	out = &Page{URL: page.String(), Title: strings.TrimSuffix(path.Base(page.Path), ".pdf")}
	defer func() {
		if recover() != nil {
			out.Text, out.Markdown = "", ""
		}
	}()

	data, err := io.ReadAll(io.LimitReader(body, maxPDFBytes))
	if err != nil {
		return out
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return out
	}
	if title := strings.TrimSpace(r.Trailer().Key("Info").Key("Title").Text()); title != "" {
		out.Title = title
	}

	var pages []string
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		if text = strings.TrimSpace(text); text != "" {
			pages = append(pages, text)
		}
	}
	out.Text = strings.Join(pages, "\n\n")
	out.Markdown = out.Text
	return out
}
