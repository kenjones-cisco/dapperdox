package handlers_test

import (
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
}
