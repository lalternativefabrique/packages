package appkeys_test

import (
	"context"
	"net/http"

	"github.com/lalternative/packages/go/appkeys"
	"github.com/lalternative/packages/go/svcauth"
)

func Example() {
	keys, err := appkeys.New(appkeys.Config{
		Product:   "tornad",
		Urbangate: "https://id.urbangate.dev",
		Provisioner: svcauth.HydraClientCredentials(
			"https://id.urbangate.dev",
			"tornad-provisioner", "s3cret",
			[]string{"urbangate"},
			[]string{"urbangate:keys:issue"},
		),
		OwnerOf:       identityIDOfSession,
		DefaultScopes: []string{"search"},
	})
	if err != nil {
		panic(err)
	}

	// Run keeps the revocation list fresh. Require refuses keys until it has
	// answered once, so start it before serving.
	go keys.Run(context.Background())

	mux := http.NewServeMux()
	mux.Handle("/api/keys", http.StripPrefix("/api/keys", keys.Relay()))
	mux.Handle("/api/keys/", http.StripPrefix("/api/keys", keys.Relay()))
	mux.Handle("/search", keys.Require("tornad:search")(searchHandler()))
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !keys.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func searchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := appkeys.ClaimsFrom(r.Context())
		_ = claims.Owner    // the person the key belongs to
		_ = claims.ClientID // the key's own id, stable across rotations
	})
}

func identityIDOfSession(r *http.Request) (string, bool) {
	// Whatever the product's own session tells it, as long as the value is the
	// provider identity id: urbangate names a key's owner by that, not by the
	// product's local user id. @lalternative/auth writes it to user.identityId
	// on every single sign-on.
	return "", false
}
