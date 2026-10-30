// Package apitest loads the OpenAPI document for tests, so that the
// validator stays out of the server binary.
package apitest

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/sourcehawk/pricedin/server/api"
)

// Load parses the document and resolves its references, or fails the
// test.
func Load(tb testing.TB) *openapi3.T {
	tb.Helper()
	loader := openapi3.NewLoader()
	loader.Context = context.Background()
	doc, err := loader.LoadFromData(api.Document())
	if err != nil {
		tb.Fatalf("load openapi document: %v", err)
	}
	return doc
}
