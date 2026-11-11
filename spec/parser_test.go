package spec_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/spec"
)

func TestParserInstancesOwnSettingsAndStatusCodes(t *testing.T) {
	raw := []byte(`{"swagger":"2.0","info":{"title":"Owned API","version":"1"},"paths":{"/pets":{"get":{"summary":"List pets","responses":{"200":{"description":"Found"}}}}}}`)

	type fixture struct {
		parser       *spec.Parser
		host, status string
	}

	fixtures := make([]fixture, 0, 2)

	for _, name := range []string{"first", "second"} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "status_codes.csv"), []byte("200,"+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(root, "swagger.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}

		settings := config.Settings{
			AssetsDir: root, SpecDir: root, SpecFilename: []string{"swagger.json"},
			SpecDefaultHost: name + ".example", SpecRewriteURL: map[string]string{"Owned API": name + " API"},
		}

		parser, err := spec.NewParser(settings)
		if err != nil {
			t.Fatal(err)
		}

		settings.SpecRewriteURL["Owned API"] = "mutated"
		settings.SpecFilename[0] = "missing.json"

		fixtures = append(fixtures, fixture{parser, name + ".example", name})
	}

	var wg sync.WaitGroup
	for _, value := range fixtures {
		wg.Go(func() {
			for range 20 {
				suite, err := value.parser.LoadSuite()
				if err != nil {
					t.Error(err)

					return
				}

				api := suite.Specifications[value.status+"-api"]
				if api == nil || api.APIInfo.Title != value.status+" API" {
					t.Error("static rewrite policy leaked between parser instances")

					return
				}

				api, err = value.parser.Parse("/api.json", raw)
				if err != nil {
					t.Error(err)

					return
				}

				method := api.APIs[0].Methods[0]
				if api.APIs[0].URL.Host != value.host || method.Responses[200].StatusDescription != value.status {
					t.Errorf("parser state leaked: host=%q status=%q", api.APIs[0].URL.Host, method.Responses[200].StatusDescription)
				}

				if !strings.HasPrefix(api.APIInfo.Title, "Owned") {
					t.Error("discovered specification unexpectedly applied static file rewrites")
				}
			}
		})
	}

	wg.Wait()
}
