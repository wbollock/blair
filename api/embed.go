// Package api embeds the OpenAPI contract (openapi.yaml), the source of truth for /api/v1.
package api

import (
	_ "embed"
	"encoding/json"

	"github.com/getkin/kin-openapi/openapi3"
)

//go:embed openapi.yaml
var YAML []byte

// Load parses and validates the contract.
func Load() (*openapi3.T, error) {
	doc, err := openapi3.NewLoader().LoadFromData(YAML)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// JSON returns the contract rendered as JSON (served at /api/openapi.json).
func JSON() ([]byte, error) {
	doc, err := Load()
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}
