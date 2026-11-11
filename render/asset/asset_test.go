package asset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
)

func catalogFixture(t *testing.T, files map[string]string) {
	t.Helper()

	root := filepath.Join("asset-test-work", t.Name())
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root); _ = os.Remove("asset-test-work"); viper.Reset() })

	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	viper.Set(config.DefaultAssetsDir, "../../assets")
	viper.Set(config.AssetsDir, root)
	viper.Set(config.Theme, "default")
}

func TestGFMCompilationIsIdempotent(t *testing.T) {
	catalogFixture(t, map[string]string{"gfm.map": "<h1>:<h1 class=\"once\">", "templates/guide.md": "# Heading"})

	c, err := NewCatalog()
	if err != nil {
		t.Fatal(err)
	}

	for range 100 {
		if compileErr := c.compileGFMMap(); compileErr != nil {
			t.Fatal(compileErr)
		}

		if c.RuleCount() != 1 {
			t.Fatalf("duplicate markdown rules: %d", c.RuleCount())
		}

		got := string(c.processMarkdown([]byte("# Heading")))
		if strings.Count(got, `class="once"`) != 1 {
			t.Fatalf("mapping changed: %q", got)
		}
	}

	other, err := NewCatalog()
	if err != nil {
		t.Fatal(err)
	}

	if other.RuleCount() != c.RuleCount() {
		t.Fatal("catalog construction accumulated rules")
	}
}

func TestCatalogPrecedenceMetadataAndFutureSections(t *testing.T) {
	catalogFixture(t, map[string]string{
		"templates/guide.md":                        "Navigation: Custom\nSortOrder: 3\n\n# Custom",
		"themes/default/templates/guide.tmpl":       "lower-priority",
		"sections/future/templates/guides/start.md": "# Future",
		"sections/future/static/example.txt":        "future-static",
		"static/custom.txt":                         "custom",
	})

	c, err := NewCatalog()
	if err != nil {
		t.Fatal(err)
	}

	name := "assets/templates/guide.tmpl"

	data, err := c.Asset(name)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "Custom") || strings.Contains(string(data), "lower-priority") {
		t.Fatal("custom template precedence changed")
	}

	if c.MetaData(name, "navigation") != "Custom" || c.MetaData(name, "SortOrder") != "3" {
		t.Fatal("guide metadata lost")
	}

	data[0] = 'X'

	again, _ := c.Asset(name)
	if again[0] == 'X' {
		t.Fatal("caller mutated catalog bytes")
	}

	for _, name := range []string{"assets/templates/future/templates/guides/start.tmpl", "assets/static/future/static/example.txt"} {
		if _, err := c.Asset(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCatalogCompileErrorsReturn(t *testing.T) {
	for _, test := range []struct{ name, path, content string }{
		{"regex", "gfm.map", "[:bad"},
		{"overlay", "templates/guide.md", "Overlay: true\n\nno sections"},
		{"html", "templates/guide.html", "<p>bad extension</p>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalogFixture(t, map[string]string{test.path: test.content})

			if _, err := NewCatalog(); err == nil {
				t.Fatal("invalid deployment asset accepted")
			}
		})
	}
}

func TestMetadataEmptyKeyAndLargeContent(t *testing.T) {
	text := ": value\n" + strings.Repeat("x", 100000)

	body, _ := processMetadata([]byte(text))
	if !strings.HasPrefix(string(body), ": value\n") || len(body) < 100000 {
		t.Fatal("metadata processing lost content")
	}
}
