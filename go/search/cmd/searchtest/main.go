// Command searchtest is a local HTTP server for exercising the search
// library against a real SearXNG instance without wiring it into any app.
//
//	kubectl --kubeconfig ~/.kube/kube-ovh-v1.yml -n ai port-forward svc/searxng 8888:8080
//	SEARXNG_URL=http://localhost:8888 go run ./cmd/searchtest
//	curl 'localhost:8090/search?q=gramsci&category=general'
//	curl 'localhost:8090/fetch?url=https://example.com/article'
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/lalternative/packages/go/search"
	"github.com/lalternative/packages/go/search/fetch"
	"github.com/lalternative/packages/go/search/searxng"
)

func main() {
	searxngURL := os.Getenv("SEARXNG_URL")
	if searxngURL == "" {
		searxngURL = "http://localhost:8888"
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8090"
	}

	provider := searxng.New(searxngURL, nil)
	cache := fetch.NewMemoryCache(15 * time.Minute)

	mux := http.NewServeMux()
	mux.HandleFunc("/search", searchHandler(provider))
	mux.HandleFunc("/fetch", fetchHandler(cache))

	log.Printf("searchtest: SEARXNG_URL=%s, listening on %s", searxngURL, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func searchHandler(provider *searxng.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			http.Error(w, `{"error":"q parameter is required"}`, http.StatusBadRequest)
			return
		}

		category := search.Category(r.URL.Query().Get("category"))
		if category == "" {
			category = search.CategoryGeneral
		} else if category == "academic" {
			category = search.CategoryAcademic
		}

		limit := 10
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil {
				limit = n
			}
		}

		res, err := provider.Search(r.Context(), search.Query{
			Text:     q,
			Category: category,
			Limit:    limit,
			Language: r.URL.Query().Get("language"),
		})
		if err != nil {
			http.Error(w, `{"error":`+strconv.Quote(err.Error())+`}`, http.StatusBadGateway)
			return
		}

		writeJSON(w, res)
	}
}

func fetchHandler(cache fetch.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawURL := r.URL.Query().Get("url")
		if rawURL == "" {
			http.Error(w, `{"error":"url parameter is required"}`, http.StatusBadRequest)
			return
		}

		maxRunes := 6000
		if m := r.URL.Query().Get("max_runes"); m != "" {
			if n, err := strconv.Atoi(m); err == nil {
				maxRunes = n
			}
		}

		page, err := fetch.FetchStatic(r.Context(), rawURL, maxRunes, cache)
		if err != nil {
			http.Error(w, `{"error":`+strconv.Quote(err.Error())+`}`, http.StatusBadGateway)
			return
		}

		writeJSON(w, page)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
