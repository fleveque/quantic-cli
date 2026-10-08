// Package api talks to Quantic's /api/v1 (design §2).
//
// api.gen.go is generated from api/openapi.json, Quantic's own OpenAPI
// document, by oapi-codegen; never edit it by hand. overlay.yaml renames its
// operations and widens its numbers before generation (see the file).
// client.go and retry.go are the thin wrapper the commands use: one method per
// endpoint, typed errors, and retrying a rate-limited request.
//
// To follow a change in the API:
//
//	curl -sf https://quantic.finance/api/v1/openapi.json -o api/openapi.json
//	go generate ./internal/api
package api

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../api/openapi.json
