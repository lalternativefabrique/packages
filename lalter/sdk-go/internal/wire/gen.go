// Package wire is the generated transport for the lalter API: every path,
// method and parameter, produced from lalter's own contract so none of them
// is written by hand.
//
// Internal on purpose. These methods return raw *http.Response and generated
// pointer types, which is not an API to hand a consumer — the parent package
// wraps them with typed errors and the conversions that keep "not found"
// distinguishable from "no answer".
package wire

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -config oapi-codegen.yaml ../../openapi/lalter.json
