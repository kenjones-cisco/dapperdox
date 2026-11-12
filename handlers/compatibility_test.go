package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/handlers"
)

func TestDocumentationCompatibility(t *testing.T) {
	config.Restore()
	t.Cleanup(config.Restore)
	viper.Set(config.DefaultAssetsDir, "../assets")
	viper.Set(config.AssetsDir, "../fixtures/compatibility/assets")
	viper.Set(config.Theme, "default")
	viper.Set(config.SpecDir, "../fixtures/compatibility")
	viper.Set(config.SpecFilename, []string{"swagger.json"})

	settings := config.Snapshot()
	settings.TLSCert, settings.TLSKey = "fixture-cert", "fixture-key"
	settings.AllowOrigin = []string{"https://client.example"}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, req.Method+" "+req.URL.RequestURI())
	}))
	t.Cleanup(upstream.Close)
	settings.ProxyPath = map[string]string{"/_proxy": upstream.URL}

	router, err := handlers.NewRouterChainWithConfig(settings)
	if err != nil {
		t.Fatal(err)
	}

	settings.SpecFilename[0] = "missing.json"

	config.Restore()
	viper.Set(config.DefaultAssetsDir, "missing-assets")
	viper.Set(config.ForceSpecList, true)
	viper.Set(config.ShowAssets, true)

	for _, test := range []struct {
		path     string
		status   int
		location string
		contains []string
	}{
		{"/", http.StatusFound, "/compatibility-api/reference", nil},
		{"/compatibility-api", http.StatusFound, "/compatibility-api/", nil},
		{"/guides", http.StatusFound, "/guides/overview", nil},
		{"/guides/overview", http.StatusOK, "", []string{`name="stable-heading"`, `href="#stable-heading"`, "<strong>rendered guide</strong>", "<table", `type="checkbox"`, `class="highlight highlight-Go"`}},
		{"/compatibility-api/reference", http.StatusOK, "", []string{`id="specific-overlay"`, "/compatibility-api/reference/pet-operations/update-pet"}},
		{"/compatibility-api/reference/pet-operations/update-pet", http.StatusOK, "", []string{`id="apiexplorer"`, `apiExplorer.setBeforeSendCallback`, `id="payload"`}},
		{"/swagger.json", http.StatusOK, "", []string{`"title": "Compatibility API"`}},
		{"/missing", http.StatusNotFound, "", []string{"Page not found"}},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, http.NoBody))

			if response.Code != test.status || response.Header().Get("Location") != test.location {
				t.Fatalf("status/location = %d %q", response.Code, response.Header().Get("Location"))
			}

			if test.status != http.StatusNotFound && response.Header().Get("Strict-Transport-Security") == "" {
				t.Fatal("router lost instance-owned TLS configuration")
			}

			for _, want := range test.contains {
				if !strings.Contains(response.Body.String(), want) {
					t.Errorf("response missing %q", want)
				}
			}

			if test.path == "/compatibility-api/reference" && strings.Contains(response.Body.String(), `id="global-overlay"`) {
				t.Fatal("global overlay replaced the specific section")
			}
		})
	}

	for _, file := range []struct {
		path, contentType, signature string
	}{
		{"/browser/files/open-sans-latin-400-normal.woff2", "font/woff2", "wOF2"},
		{"/browser/files/open-sans-latin-400-normal.woff", "font/woff", "wOFF"},
		{"/browser/browser.js", "application/javascript", ""},
		{"/js/explorer.js", "application/javascript", ""},
	} {
		t.Run(file.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, file.path, http.NoBody))

			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != file.contentType || response.Body.Len() == 0 || !strings.HasPrefix(response.Body.String(), file.signature) {
				t.Fatalf("asset not served with valid bytes and MIME type: status=%d type=%q", response.Code, response.Header().Get("Content-Type"))
			}
		})
	}

	for _, test := range []struct {
		name, method, path, origin  string
		status                      int
		location, body, allowOrigin string
		protected                   bool
	}{
		{"GET-only HEAD", http.MethodHead, "/swagger.json", "", http.StatusMethodNotAllowed, "", "", "", false},
		{"GET-only POST", http.MethodPost, "/swagger.json", "", http.StatusMethodNotAllowed, "", "", "", false},
		{"GET-only OPTIONS", http.MethodOptions, "/swagger.json", "", http.StatusMethodNotAllowed, "", "", "", false},
		{"trailing slash", http.MethodGet, "/swagger.json/", "", http.StatusNotFound, "", "Page not found", "", false},
		{"path cleaning", http.MethodGet, "/guides//overview?v=1", "", http.StatusMovedPermanently, "/guides/overview?v=1", "", "", false},
		{"escaped path", http.MethodGet, "/guides/%6fverview", "", http.StatusOK, "", "rendered guide", "", true},
		{"escaped slash", http.MethodGet, "/guides%2foverview", "", http.StatusOK, "", "rendered guide", "", true},
		{"allowed CORS", http.MethodGet, "/swagger.json", "https://client.example", http.StatusOK, "", "", "https://client.example", true},
		{"disallowed CORS", http.MethodGet, "/swagger.json", "https://other.example", http.StatusOK, "", "", "", true},
		{"missing path CORS", http.MethodGet, "/missing", "https://client.example", http.StatusNotFound, "", "Page not found", "", false},
		{"prefix without segment boundary", http.MethodGet, "/_proxysuffix?q=1", "", http.StatusOK, "", "GET /_proxysuffix?q=1", "", true},
		{"proxy HEAD", http.MethodHead, "/_proxy", "", http.StatusOK, "", "", "", true},
		{"CSRF failure", http.MethodPost, "/_proxy", "", http.StatusBadRequest, "", "<h1>400 - ", "", false},
		{"proxy preflight", http.MethodOptions, "/_proxy", "https://client.example", http.StatusOK, "", "", "https://client.example", true},
		{"health bypasses middleware", http.MethodPost, "/healthz", "", http.StatusOK, "", "", "", false},
		{"readiness bypasses middleware", http.MethodHead, "/readyz", "", http.StatusOK, "", "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), test.method, test.path, http.NoBody)
			req.Header.Set("Origin", test.origin)

			if test.method == http.MethodOptions {
				req.Header.Set("Access-Control-Request-Method", http.MethodGet)
			}

			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)

			if response.Code != test.status || response.Header().Get("Location") != test.location {
				t.Fatalf("status/location = %d %q, want %d %q", response.Code, response.Header().Get("Location"), test.status, test.location)
			}

			if !strings.Contains(response.Body.String(), test.body) {
				t.Errorf("body missing %q: %s", test.body, response.Body.String())
			}

			if got := response.Header().Get("Access-Control-Allow-Origin"); got != test.allowOrigin {
				t.Errorf("CORS origin = %q, want %q", got, test.allowOrigin)
			}

			if got := response.Header().Get("Server") != ""; got != test.protected {
				t.Errorf("server header present = %t, want %t", got, test.protected)
			}
		})
	}
}
