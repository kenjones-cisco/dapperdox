package spec

import (
	"os"
	"testing"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
)

func TestParseOwnedSuite(t *testing.T) {
	config.Restore()
	defer config.Restore()

	viper.Set(config.DefaultAssetsDir, "../assets")

	raw, err := os.ReadFile("../fixtures/common_api.json")
	if err != nil {
		t.Fatal(err)
	}

	api, err := Parse("/common/api.json", raw)
	if err != nil {
		t.Fatal(err)
	}

	suite, err := NewSuite(map[string]*APISpecification{"common": api})
	if err != nil || len(suite.Specifications) != 1 {
		t.Fatalf("owned suite: %v", err)
	}

	if _, err := NewSuite(map[string]*APISpecification{"one": api, "two": api}); err == nil {
		t.Fatal("duplicate public API identifier accepted")
	}
}

func TestParseInvalidSpecificationReturnsError(t *testing.T) {
	config.Restore()
	defer config.Restore()

	for _, raw := range []string{
		"{",
		`{"swagger":"2.0","paths":{}}`,
		`{"swagger":"2.0","info":{"title":"Bad","version":"1"}}`,
		`{"swagger":"2.0","info":{"title":"Bad","version":"1"},"paths":{"/test":{"get":{"summary":"Bad"}}}}`,
	} {
		if _, err := Parse("/bad/api.json", []byte(raw)); err == nil {
			t.Errorf("accepted invalid specification: %s", raw)
		}
	}
}
