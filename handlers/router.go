// Package handlers provides the API handlers for the service.
package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/justinas/nosurf"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/handlers/guides"
	"github.com/kenjones-cisco/dapperdox/handlers/home"
	"github.com/kenjones-cisco/dapperdox/handlers/proxy"
	"github.com/kenjones-cisco/dapperdox/handlers/reference"
	"github.com/kenjones-cisco/dapperdox/handlers/specs"
	"github.com/kenjones-cisco/dapperdox/handlers/static"
	"github.com/kenjones-cisco/dapperdox/handlers/timeout"
	log "github.com/kenjones-cisco/dapperdox/logger"
	"github.com/kenjones-cisco/dapperdox/render"
	"github.com/kenjones-cisco/dapperdox/render/asset"
	"github.com/kenjones-cisco/dapperdox/spec"
	"github.com/kenjones-cisco/dapperdox/version"
)

var errDuplicateRoute = errors.New("duplicate route")

// NewRouterChain creates a router with a chain of middlewares that acts as an http.Handler.
func NewRouterChain() (http.Handler, error) {
	return NewRouterChainWithConfig(config.Snapshot())
}

// NewRouterChainWithConfig creates a router from immutable startup settings.
func NewRouterChainWithConfig(settings config.Settings) (http.Handler, error) {
	settings = settings.Clone()

	catalog, err := asset.NewCatalogWithConfig(settings)
	if err != nil {
		return nil, err
	}

	parser, err := spec.NewParser(settings)
	if err != nil {
		return nil, err
	}

	suite, err := parser.LoadSuite()
	if err != nil {
		return nil, err
	}

	generation, err := buildGenerationWithConfig(suite, nil, catalog, settings)
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/healthz" || req.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusOK)

			return
		}

		generation.router.ServeHTTP(w, req)
	}), nil
}

func createMiddlewareRouter(renderer *render.Context, settings config.Settings) *mux.Router {
	router := mux.NewRouter()
	router.Use(
		handlers.RecoveryHandler(handlers.RecoveryLogger(recoveryLogger{}), handlers.PrintRecoveryStack(true)),
		withLogger,
		func(h http.Handler) http.Handler { return timeoutHandler(renderer, h) },
		func(h http.Handler) http.Handler { return withCsrf(renderer, h) },
		func(h http.Handler) http.Handler { return injectHeadersWithConfig(h, settings) },
		handlers.CORS(handlers.AllowedOrigins(settings.AllowOrigin)),
	)

	router.PathPrefix("/debug/pprof/").HandlerFunc(pprof.Index)

	return router
}

type recoveryLogger struct{}

func (recoveryLogger) Println(values ...any) {
	log.Logger().Error("HTTP handler panic", slog.String("details", fmt.Sprintln(values...)))
}

func buildGenerationWithConfig(suite *spec.Suite, raw map[string][]byte, catalog *asset.Catalog, settings config.Settings) (*generation, error) {
	renderer, err := render.NewWithConfig(suite, catalog, settings)
	if err != nil {
		return nil, err
	}

	router := createMiddlewareRouter(renderer, settings)

	err = specs.RegisterWithConfig(router, raw, settings)
	if err != nil {
		return nil, err
	}

	reference.Register(router, renderer)

	err = guides.Register(router, renderer)
	if err != nil {
		return nil, err
	}

	static.Register(router, renderer)
	home.RegisterWithConfig(router, renderer, settings)
	proxy.RegisterWithConfig(router, settings)

	count := 0
	paths := make(map[string]bool)

	err = router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if routeErr := route.GetError(); routeErr != nil {
			return routeErr
		}

		path, pathErr := route.GetPathTemplate()
		if pathErr != nil {
			return pathErr
		}

		if paths[path] {
			return fmt.Errorf("%w %q", errDuplicateRoute, path)
		}

		paths[path] = true
		count++

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &generation{router: router, routes: count}, nil
}

func withLogger(h http.Handler) http.Handler {
	return handlers.CombinedLoggingHandler(os.Stdout, h)
}

func withCsrf(renderer *render.Context, h http.Handler) http.Handler {
	csrfHandler := nosurf.New(h)
	csrfHandler.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rsn := nosurf.Reason(req).Error()
		log.Logger().WarnContext(req.Context(), fmt.Sprintf("failed csrf validation: %s", rsn))
		renderer.HTML(w, http.StatusBadRequest, "error", map[string]any{"error": rsn})
	}))

	return csrfHandler
}

func timeoutHandler(renderer *render.Context, h http.Handler) http.Handler {
	return timeout.Handler(h, 1*time.Second, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Logger().WarnContext(req.Context(), "request timed out")
		renderer.HTML(w, http.StatusRequestTimeout, "error", map[string]any{"error": "Request timed out"})
	}))
}

// Handle additional headers such as strict transport security for TLS, and
// giving the Server name.
func injectHeadersWithConfig(h http.Handler, settings config.Settings) http.Handler {
	tlsEnabled := settings.TLSCert != "" && settings.TLSKey != ""

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Server", fmt.Sprintf("%s %s", version.ProductName, version.Version))

		if tlsEnabled {
			w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}

		h.ServeHTTP(w, r)
	})
}
