package api

import _ "embed"

// Spec is the API's OpenAPI 3.1 description, served at
// /api/v1/openapi.yaml and held to the handlers by the contract tests.
//
//go:embed openapi.yaml
var Spec []byte
