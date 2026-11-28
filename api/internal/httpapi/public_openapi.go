package httpapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"gopkg.in/yaml.v3"
)

// publicOpenAPISpec documents the public read API. A test keeps it in sync
// with registered routes and response types; update both in one change.
//
//go:embed public_openapi.yaml
var publicOpenAPISpec []byte

// renderPublicOpenAPI converts the embedded spec to JSON with the site origin
// as its only server, so clients target the deployment that served it.
func renderPublicOpenAPI(origin string) ([]byte, error) {
	var document map[string]any
	if err := yaml.Unmarshal(publicOpenAPISpec, &document); err != nil {
		return nil, fmt.Errorf("parse public OpenAPI spec: %w", err)
	}
	document["servers"] = []any{map[string]any{"url": origin}}
	return json.Marshal(document)
}

func (api *API) publicOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(api.openAPI)
}
