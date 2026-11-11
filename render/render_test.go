package render

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	unrolled "github.com/unrolled/render"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/navigation"
	"github.com/kenjones-cisco/dapperdox/render/asset"
	"github.com/kenjones-cisco/dapperdox/spec"
)

func testContext(t *testing.T, files map[string]string) *Context {
	t.Helper()

	root := filepath.Join("render-test-work", t.Name())
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root); _ = os.Remove("render-test-work"); viper.Reset() })

	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	viper.Set(config.DefaultAssetsDir, "../assets")
	viper.Set(config.AssetsDir, root)
	viper.Set(config.Theme, "default")

	catalog, err := asset.NewCatalog()
	if err != nil {
		t.Fatal(err)
	}

	ctx, err := New(&spec.Suite{Specifications: map[string]*spec.APISpecification{}, Groups: map[string][]*spec.APISpecification{}}, catalog)
	if err != nil {
		t.Fatal(err)
	}

	return ctx
}

func TestConcurrentRenderCounterAndNestedOverlays(t *testing.T) {
	ctx := testContext(t, map[string]string{
		"templates/layout.tmpl": `<body>[: yield :]</body>`,
		"templates/page.tmpl":   `[: counter_add 1 :]|[: overlay "outer" . :]|[: counter_add 1 :]`,
		"templates/reference/specification_list/outer/overlay.tmpl": `[: counter_add 1 :]/[: overlay "inner" . :]`,
		"templates/reference/specification_list/inner/overlay.tmpl": `[: counter_add 1 :]`,
	})
	rules, names := ctx.Assets.RuleCount(), ctx.Assets.Names()

	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 20 {
				var output bytes.Buffer

				ctx.HTML(&output, http.StatusOK, "page", map[string]interface{}{"SpecificationList": true})

				if got := strings.ReplaceAll(output.String(), "\n", ""); got != "<body>1|2/3|4</body>" {
					t.Errorf("counter or overlay changed: %q", got)

					return
				}
			}
		}()
	}

	wg.Wait()

	if ctx.Assets.RuleCount() != rules || strings.Join(ctx.Assets.Names(), "\n") != strings.Join(names, "\n") {
		t.Fatal("rendering changed immutable catalog")
	}
}

func TestOverlayPrecedenceAndFutureSections(t *testing.T) {
	ctx := testContext(t, map[string]string{
		"templates/layout.tmpl": `[: yield :]`,
		"templates/page.tmpl":   `[: overlay "banner" . :]`,
		"templates/reference/specification_summary/banner/overlay.tmpl":                 "global",
		"sections/future/templates/reference/specification_summary/banner/overlay.tmpl": "specific",
	})

	var output bytes.Buffer

	ctx.HTML(&output, http.StatusOK, "page", map[string]interface{}{"SpecificationSummary": true, "ID": "future"})

	if strings.TrimSpace(output.String()) != "specific" {
		t.Fatalf("section overlay did not take precedence: %q", output.String())
	}

	output.Reset()
	ctx.HTML(&output, http.StatusOK, "page", map[string]interface{}{"SpecificationSummary": true, "ID": "other"})

	if strings.TrimSpace(output.String()) != "global" {
		t.Fatalf("global fallback changed: %q", output.String())
	}
}

func TestStatusWarningCustomLayoutAndRequestIsolation(t *testing.T) {
	ctx := testContext(t, map[string]string{
		"templates/layout.tmpl": `<html><head><script>var body = "<body>";</script></head><BODY class="custom" data-sign=">">[: yield :]</BODY></html>`,
		"templates/page.tmpl":   `content`,
	})
	last := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	status := map[string]Status{
		"one": {State: "stale", Name: "<script>alert(1)</script>\nAPI", LastSuccess: last},
		"two": {State: "stale", Name: "Other API", LastSuccess: last},
	}
	request := WithStatus(httptest.NewRequest(http.MethodGet, "/", http.NoBody), status)
	vars := ctx.DefaultVars(request, &spec.APISpecification{ID: "one"}, nil)
	w := httptest.NewRecorder()
	ctx.HTML(w, http.StatusOK, "page", vars)
	body := w.Body.String()

	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("HTML must not be cached")
	}

	for _, want := range []string{`<BODY class="custom" data-sign=">"><div`, `data-documentation-state="stale"`, "&lt;script&gt;", last.Format(time.RFC3339), "content"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %q", want, body)
		}
	}

	if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, "Other API") {
		t.Fatalf("unsafe or unrelated warning: %q", body)
	}

	w = httptest.NewRecorder()
	ctx.HTML(w, http.StatusOK, "page", ctx.DefaultVars(httptest.NewRequest(http.MethodGet, "/", http.NoBody), nil, nil))

	if strings.Contains(w.Body.String(), "documentation-status-warning") {
		t.Fatal("request status leaked")
	}

	w = httptest.NewRecorder()
	ctx.HTML(w, http.StatusOK, "page", vars, unrolled.HTMLOptions{Layout: ""})

	if !strings.HasPrefix(w.Body.String(), `<div class="documentation-status-warning"`) {
		t.Fatal("layout-free warning missing")
	}
}

func TestContextsOwnSuiteAndNavigation(t *testing.T) {
	ctx := testContext(t, nil)

	other, err := New(&spec.Suite{Specifications: map[string]*spec.APISpecification{"other": {ID: "other"}}}, ctx.Assets)
	if err != nil {
		t.Fatal(err)
	}

	ctx.SetGuidesNavigation(nil, []*navigation.Node{{}})

	if len(ctx.DefaultVars(nil, nil, nil)["NavigationGuides"].(GuideType)) != 1 {
		t.Fatal("missing guides")
	}

	if len(other.DefaultVars(nil, nil, nil)["NavigationGuides"].(GuideType)) != 0 {
		t.Fatal("navigation leaked across contexts")
	}

	if len(ctx.DefaultVars(nil, nil, nil)["APISuite"].(map[string]*spec.APISpecification)) != 0 {
		t.Fatal("suite leaked across contexts")
	}
}

func TestTemplateCompilationAndExecutionErrors(t *testing.T) {
	ctx := testContext(t, map[string]string{
		"templates/layout.tmpl": `[: yield :]`,
		"templates/page.tmpl":   `<span>before[: mod 1 0 :]after</span>`,
	})
	w := httptest.NewRecorder()
	ctx.HTML(w, http.StatusOK, "page", nil)

	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "before") {
		t.Fatalf("partial output served: %d %q", w.Code, w.Body.String())
	}

	root := filepath.Join("render-test-work", t.Name(), "templates", "bad.tmpl")
	if err := os.WriteFile(root, []byte("[: if :]"), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := asset.NewCatalog()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := New(ctx.Suite, catalog); err == nil {
		t.Fatal("invalid template accepted")
	}
}

func TestPackagedThemes(t *testing.T) {
	for _, theme := range []string{"default", "sectionbar"} {
		t.Run(theme, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			viper.Set(config.DefaultAssetsDir, "../assets")
			viper.Set(config.Theme, theme)

			catalog, err := asset.NewCatalog()
			if err != nil {
				t.Fatal(err)
			}

			ctx, err := New(&spec.Suite{}, catalog)
			if err != nil {
				t.Fatal(err)
			}

			var b bytes.Buffer

			ctx.HTML(&b, http.StatusNotFound, "error", map[string]interface{}{"error": "Not found", "code": http.StatusNotFound})

			if !strings.Contains(b.String(), "Not found") {
				t.Fatalf("theme %s failed: %s", theme, b.String())
			}
		})
	}
}

// TestConsumingCiscoTheme optionally validates a real consuming documentation
// checkout: DAPPERDOX_TEST_ASSETS must point to its deployment assets directory.
func TestConsumingCiscoTheme(t *testing.T) {
	root := os.Getenv("DAPPERDOX_TEST_ASSETS")
	if root == "" {
		t.Skip("set DAPPERDOX_TEST_ASSETS to exercise the consuming Cisco theme")
	}

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set(config.DefaultAssetsDir, "../assets")
	viper.Set(config.AssetsDir, root)
	viper.Set(config.Theme, "cisco")

	catalog, err := asset.NewCatalog()
	if err != nil {
		t.Fatal(err)
	}

	suite := &spec.Suite{
		Specifications: map[string]*spec.APISpecification{},
		Groups:         map[string][]*spec.APISpecification{},
	}

	ctx, err := New(suite, catalog)
	if err != nil {
		t.Fatal(err)
	}

	last := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	request := WithStatus(httptest.NewRequest(http.MethodGet, "/", http.NoBody), map[string]Status{
		"stale-api": {State: "stale", Name: "<Stale API>", LastSuccess: last},
	})
	binding := ctx.DefaultVars(request, nil, Vars{"Title": "Specifications list", "SpecificationList": true})
	cleanBinding := ctx.DefaultVars(nil, nil, Vars{"Title": "Specifications list", "SpecificationList": true})

	clean, err := ctx.html("specification_list", cleanBinding)
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	ctx.HTML(response, http.StatusOK, "specification_list", binding)

	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected response: %d %v", response.Code, response.Header())
	}

	body := response.Body.String()
	for _, marker := range []string{
		`href="/images/runon.png"`,
		"RunOn Multicloud Manager API Suite",
		"documentation-status-warning",
		"&lt;Stale API&gt;",
		last.Format(time.RFC3339),
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("custom output missing %q", marker)
		}
	}

	start := strings.Index(body, `<div class="documentation-status-warning"`)
	if start < 0 {
		t.Fatal("stale warning missing")
	}

	end := strings.Index(body[start:], "</div>")
	if end < 0 {
		t.Fatal("unterminated warning")
	}

	withoutWarning := body[:start] + body[start+end+len("</div>"):]
	if withoutWarning != string(clean) {
		t.Fatal("stale warning changed unrelated custom theme output")
	}
}
