package handlers

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/justinas/nosurf"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover"
	"github.com/kenjones-cisco/dapperdox/handlers/guides"
	"github.com/kenjones-cisco/dapperdox/handlers/home"
	"github.com/kenjones-cisco/dapperdox/handlers/proxy"
	"github.com/kenjones-cisco/dapperdox/handlers/reference"
	"github.com/kenjones-cisco/dapperdox/handlers/specs"
	"github.com/kenjones-cisco/dapperdox/handlers/static"
	"github.com/kenjones-cisco/dapperdox/handlers/timeout"
	log "github.com/kenjones-cisco/dapperdox/logger"
	"github.com/kenjones-cisco/dapperdox/render"
	"github.com/kenjones-cisco/dapperdox/spec"
	"github.com/kenjones-cisco/dapperdox/version"
)

// NewRouterChain creates a router with a chain of middlewares that acts as an http.Handler.
func NewRouterChain() http.Handler {
	router := createMiddlewareRouter()

	loadAndRegisterSpecs(router, nil)

	return router
}

func createMiddlewareRouter() *mux.Router {
	router := mux.NewRouter()
	router.Use(
		handlers.RecoveryHandler(handlers.RecoveryLogger(log.Logger()), handlers.PrintRecoveryStack(true)),
		withLogger,
		timeoutHandler,
		withCsrf,
		injectHeaders,
		handlers.CORS(handlers.AllowedOrigins(viper.GetStringSlice(config.AllowOrigin))),
	)

	return router
}

func loadAndRegisterSpecs(router *mux.Router, d discover.DiscoveryManager) {
	specs.Register(router, d)

	if err := spec.LoadSpecifications(d); err != nil {
		log.Logger().Fatalf("Load specification error: %s", err)
	}

	render.Register()

	reference.Register(router)
	guides.Register(router)
	static.Register(router)
	home.Register(router)
	proxy.Register(router)
}

func withLogger(h http.Handler) http.Handler {
	return handlers.CombinedLoggingHandler(os.Stdout, h)
}

func withCsrf(h http.Handler) http.Handler {
	csrfHandler := nosurf.New(h)
	csrfHandler.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rsn := nosurf.Reason(req).Error()
		log.Logger().Warnf("failed csrf validation: %s", rsn)
		render.HTML(w, http.StatusBadRequest, "error", map[string]interface{}{"error": rsn})
	}))

	return csrfHandler
}

func timeoutHandler(h http.Handler) http.Handler {
	return timeout.Handler(h, 1*time.Second, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Logger().Warn("request timed out")
		render.HTML(w, http.StatusRequestTimeout, "error", map[string]interface{}{"error": "Request timed out"})
	}))
}

// Handle additional headers such as strict transport security for TLS, and
// giving the Server name.
func injectHeaders(h http.Handler) http.Handler {
	tlsEnabled := viper.GetString(config.TLSCert) != "" && viper.GetString(config.TLSKey) != ""

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Server", fmt.Sprintf("%s %s", version.ProductName, version.Version))

		if tlsEnabled {
			w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}

		h.ServeHTTP(w, r)
	})
}
