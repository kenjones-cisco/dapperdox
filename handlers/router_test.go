package handlers

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/justinas/nosurf"

	"github.com/kenjones-cisco/dapperdox/config"
)

func TestMiddlewareCSRFTokenAcrossRoutes(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/token", func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, nosurf.Token(req))
	}).Methods(http.MethodGet)
	router.PathPrefix("/submit").HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	generation, err := finalizeRouter(router, middleware(nil, config.Settings{})...)
	if err != nil {
		t.Fatal(err)
	}

	page := request(generation.router, "/token")
	if page.Body.Len() == 0 {
		t.Fatal("token endpoint returned no token")
	}

	for range 3 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/submit/upload", http.NoBody)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-CSRF-Token", page.Body.String())

		for _, cookie := range page.Result().Cookies() {
			req.AddCookie(cookie)
		}

		response := httptest.NewRecorder()
		generation.router.ServeHTTP(response, req)

		if response.Code != http.StatusAccepted {
			t.Fatalf("cross-route CSRF validation failed: %d", response.Code)
		}
	}
}

func TestFinalizedRouterPreservesMatching(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/exact", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "exact")
	}).Methods(http.MethodGet)
	router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "fallback "+req.Method)
	})

	generation, err := finalizeRouter(router, injectTestHeader)
	if err != nil {
		t.Fatal(err)
	}

	if generation.routes != 2 {
		t.Fatalf("route count = %d, want 2", generation.routes)
	}

	for _, test := range []struct {
		method, body string
	}{
		{http.MethodGet, "exact"},
		{http.MethodPost, "fallback POST"},
		{http.MethodHead, "fallback HEAD"},
	} {
		response := httptest.NewRecorder()
		generation.router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), test.method, "/exact", http.NoBody))

		if response.Code != http.StatusOK || response.Body.String() != test.body || response.Header().Get("X-Middleware") != "present" {
			t.Fatalf("%s changed precedence or lost middleware: %d %q %v", test.method, response.Code, response.Body.String(), response.Header())
		}
	}
}

func injectTestHeader(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Middleware", "present")
		h.ServeHTTP(w, req)
	})
}

func TestFinalizedRouterRejectsInvalidGenerations(t *testing.T) {
	for _, test := range []struct {
		name, path string
		duplicate  bool
	}{
		{"duplicate", "/same", true},
		{"invalid pattern", "/{broken", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := mux.NewRouter()
			router.HandleFunc(test.path, func(http.ResponseWriter, *http.Request) {})

			if test.duplicate {
				router.PathPrefix(test.path).HandlerFunc(func(http.ResponseWriter, *http.Request) {})
			}

			_, err := finalizeRouter(router)
			if err == nil {
				t.Fatal("invalid routing table was accepted")
			}

			if test.duplicate && !errors.Is(err, errDuplicateRoute) {
				t.Fatalf("duplicate route error = %v", err)
			}
		})
	}
}

func BenchmarkMatchedRouteMiddleware(b *testing.B) {
	for _, compiled := range []bool{false, true} {
		name := "per-request"
		if compiled {
			name = "per-generation"
		}

		b.Run(name, func(b *testing.B) {
			router := mux.NewRouter()
			router.HandleFunc("/exact", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})

			wrappers := []mux.MiddlewareFunc{
				func(h http.Handler) http.Handler { return withCsrf(nil, h) },
				func(h http.Handler) http.Handler { return injectHeadersWithConfig(h, config.Settings{}) },
			}
			if compiled {
				if _, err := finalizeRouter(router, wrappers...); err != nil {
					b.Fatal(err)
				}
			} else {
				router.Use(wrappers...)
			}

			req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/exact", http.NoBody)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				router.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}
