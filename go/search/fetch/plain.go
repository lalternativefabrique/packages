package fetch

import (
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"strings"
)

var plainTextTypes = map[string]bool{"text/plain": true, "text/markdown": true, "text/x-markdown": true}
var plainTextExtensions = map[string]bool{".txt": true, ".md": true, ".markdown": true}

func isPlainText(contentType string, page *url.URL) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if plainTextTypes[mediaType] {
		return true
	}
	return mediaType == "" && plainTextExtensions[strings.ToLower(path.Ext(page.Path))]
}

// extractPlainText hands a text or markdown document through untouched. Run
// through the HTML path, an llms.txt or a README came back flattened with
// every "#" and "[" escaped, which is the opposite of what a model wants.
func extractPlainText(body io.Reader, page *url.URL) (*Page, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxHTMLBytes))
	if err != nil {
		return nil, fmt.Errorf("fetch page: download: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	return &Page{URL: page.String(), Title: fileTitle(page), Text: text, Markdown: text}, nil
}

// fileTitle names a document that carries no title of its own by its file
// name, or its host for a bare path.
func fileTitle(page *url.URL) string {
	base := path.Base(page.Path)
	if base == "/" || base == "." || base == "" {
		return page.Host
	}
	return strings.TrimSuffix(base, path.Ext(base))
}
