package api

import (
	"context"
	"strings"
	"testing"
)

func TestContractIsValid(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Every operation is documented: summary, operationId, and scopes for token-usable data routes.
func TestEveryOperationIsDocumented(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			name := method + " " + path
			if op.Summary == "" || op.OperationID == "" {
				t.Errorf("%s: missing summary or operationId", name)
			}
			if len(op.Responses.Map()) == 0 {
				t.Errorf("%s: no responses", name)
			}
			dataRoute := !strings.HasPrefix(path, "/auth") && !strings.HasPrefix(path, "/me") && !strings.HasPrefix(path, "/admin")
			if dataRoute && op.Extensions["x-scopes"] == nil {
				t.Errorf("%s: missing x-scopes", name)
			}
		}
	}
}
