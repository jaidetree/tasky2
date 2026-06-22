// Package server assembles the HTTP surface: a stdlib mux with the huma API
// (from which the OpenAPI spec is generated) and, in production, static serving
// of the built frontend.
package server

import (
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/jaidetree/tasky2/backend/internal/task"
)

// NewAPI builds the huma API on a fresh mux and registers the task routes. The
// returned mux serves the API plus huma's /openapi.* and /docs endpoints. svc
// may be nil when the API is built only to generate the OpenAPI spec.
func NewAPI(svc *task.Service) (*http.ServeMux, huma.API) {
	mux := http.NewServeMux()
	config := huma.DefaultConfig("Tasky API", "1.0.0")
	api := humago.New(mux, config)
	task.RegisterRoutes(api, svc)
	return mux, api
}

// New returns the full production handler: the API plus, when publicDir exists,
// a static file server for the built frontend (ADR-0005). The API's specific
// route patterns take precedence over the catch-all static handler.
func New(svc *task.Service, publicDir string) http.Handler {
	mux, _ := NewAPI(svc)
	if publicDir != "" {
		if _, err := os.Stat(publicDir); err == nil {
			mux.Handle("/", http.FileServer(http.Dir(publicDir)))
		}
	}
	return mux
}
