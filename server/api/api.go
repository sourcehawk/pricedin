// Package api holds the OpenAPI 3.1 document of the HTTP API. The document
// is the contract: the server's tests validate every response against it,
// and the web app generates its TypeScript types from it.
package api

import _ "embed"

//go:embed openapi.yaml
var document []byte

// Document returns the OpenAPI document as YAML. The caller must not
// change the slice.
func Document() []byte {
	return document
}
