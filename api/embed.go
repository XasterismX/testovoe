// Package api содержит OpenAPI-спецификацию сервиса.
package api

import _ "embed"

// Spec — содержимое openapi.yaml.
//
//go:embed openapi.yaml
var Spec []byte
