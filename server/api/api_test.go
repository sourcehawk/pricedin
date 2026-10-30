package api_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/sourcehawk/pricedin/server/api/apitest"
)

func load(t *testing.T) *openapi3.T {
	t.Helper()
	return apitest.Load(t)
}

func TestTheDocumentIsValidOpenAPI31(t *testing.T) {
	doc := load(t)
	if doc.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q, want 3.1.0", doc.OpenAPI)
	}
	if err := doc.Validate(context.Background(), openapi3.EnableExamplesValidation()); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

// visit validates value against the named component schema.
func visit(t *testing.T, doc *openapi3.T, name string, value any) {
	t.Helper()
	ref := doc.Components.Schemas[name]
	if ref == nil {
		t.Fatalf("no schema %s", name)
	}
	if err := ref.Value.VisitJSON(value, openapi3.EnableJSONSchema2020(), openapi3.MultiErrors()); err != nil {
		t.Errorf("%s does not validate: %v", name, err)
	}
}

func TestTheEngineExampleMatchesTheEngineSchemas(t *testing.T) {
	doc := load(t)
	body, err := os.ReadFile("../../testdata/engine/evaluation-example.json")
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Input struct {
			History any `json:"history"`
			Levers  any `json:"levers"`
		} `json:"input"`
		Output any `json:"output"`
	}
	if err := json.Unmarshal(body, &example); err != nil {
		t.Fatal(err)
	}
	visit(t, doc, "History", example.Input.History)
	visit(t, doc, "LeversInput", example.Input.Levers)
	visit(t, doc, "Evaluation", example.Output)
}

func TestTheEvaluationSchemaRejectsDrift(t *testing.T) {
	doc := load(t)
	body, err := os.ReadFile("../../testdata/engine/evaluation-example.json")
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(map[string]any){
		"an unknown field":         func(o map[string]any) { o["fairValue"] = 1.0 },
		"a missing field":          func(o map[string]any) { delete(o, "verdict") },
		"a null without a reason":  func(o map[string]any) { o["peNow"] = map[string]any{"value": nil} },
		"an unknown reason":        func(o map[string]any) { o["gap"] = map[string]any{"value": nil, "reason": "nope"} },
		"a value next to a reason": func(o map[string]any) { o["gap"] = map[string]any{"value": 0.1, "reason": "missing_data"} },
		"an unknown verdict":       func(o map[string]any) { o["verdict"] = map[string]any{"value": "cheap"} },
		"a nested field of the wrong type": func(o map[string]any) {
			if req, ok := o["required"].(map[string]any); ok {
				req["dividends"] = "0"
			}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var example struct {
				Output map[string]any `json:"output"`
			}
			if err := json.Unmarshal(body, &example); err != nil {
				t.Fatal(err)
			}
			mutate(example.Output)
			err := doc.Components.Schemas["Evaluation"].Value.VisitJSON(example.Output, openapi3.EnableJSONSchema2020())
			if err == nil {
				t.Error("VisitJSON() error = nil, want a validation error")
			}
		})
	}
}
