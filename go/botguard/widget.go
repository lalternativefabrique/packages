package botguard

import (
	_ "embed"
	"net/http"
)

// widgetJS is the ALTCHA widget (MIT, see widget/LICENSE-altcha.txt),
// vendored from the altcha npm package so no page loads it from a CDN.
//
//go:embed widget/altcha.min.js
var widgetJS []byte

func serveWidget(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(widgetJS)
}
