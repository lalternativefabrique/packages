// Package wire is the generated transport for the vvaves API: every path,
// method and parameter, produced from vvaves's own contract so none of them is
// written by hand.
//
// Internal on purpose. These methods return raw *http.Response and generated
// pointer types, which is not an API to hand a consumer — the parent package
// wraps them with typed errors and the conversions that keep "the page could
// not be read" distinguishable from "vvaves is down".
package wire

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -config oapi-codegen.yaml ../../openapi/vvaves.json
