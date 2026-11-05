package botguard

import (
	"net/http"
	"strings"
)

const interstitialHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Checking your browser</title>
<style>
:root{color-scheme:light dark}
body{margin:0;min-height:100vh;display:grid;place-items:center;font:16px/1.5 system-ui,sans-serif;background:Canvas;color:CanvasText}
main{max-width:22rem;padding:1.5rem;text-align:center}
p{opacity:.75}
</style>
<script type="module" src="` + PathPrefix + `widget.js"></script>
</head>
<body>
<main>
<h1>One moment</h1>
<p>Your browser is doing a short check. No data leaves this site.</p>
<altcha-widget challenge="` + PathPrefix + `challenge" auto="onload" hideFooter></altcha-widget>
<noscript><p>This check needs JavaScript.</p></noscript>
</main>
<script type="module">
document.querySelector("altcha-widget").addEventListener("verified", async (e) => {
  const r = await fetch("` + PathPrefix + `verify", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({altcha: e.detail.payload}),
    credentials: "same-origin",
  });
  if (r.ok) location.reload();
});
</script>
</body>
</html>
`

func wantsPage(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html")
}

func serveInterstitial(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusPreconditionRequired)
	_, _ = w.Write([]byte(interstitialHTML))
}
