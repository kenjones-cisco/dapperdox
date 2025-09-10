package render

import (
	"net/http"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/navigation"
	"github.com/kenjones-cisco/dapperdox/spec"
)

// Guides are per specification-id, or 'top-level'.
var guides = map[string]GuideType{}

// GuideType defines an array of Navigation for guides.
type GuideType []*navigation.Node

// Vars is a map of variables.
type Vars map[string]interface{}

// DefaultVars adds the default vars (config, specs, others....) to the data map.
func DefaultVars(_ *http.Request, s *spec.APISpecification, m Vars) map[string]any {
	if m == nil {
		log().Trace("creating new template data map")

		m = make(map[string]any)
	}

	m["Config"] = config.C
	m["APISuite"] = spec.APISuite
	m["APISuiteGroups"] = spec.APISuiteGroups

	// If we have a multiple specifications or are forcing a parent "root" page for the single specification
	// then set MultipleSpecs to true to enable navigation back to the root page.
	if viper.GetBool(config.ForceSpecList) || len(spec.APISuite) > 1 {
		m["MultipleSpecs"] = true
	}

	if s == nil {
		m["NavigationGuides"] = guides[""] // Global guides
		m["SpecPath"] = ""

		return m
	}

	// Per specification defaults
	m["NavigationGuides"] = guides[s.ID]

	m["ID"] = s.ID
	m["SpecPath"] = "/" + s.ID
	m["APIs"] = s.APIs
	m["APIVersions"] = s.APIVersions
	m["Resources"] = s.ResourceList
	m["Info"] = s.APIInfo
	m["SpecURL"] = s.URL

	return m
}

// SetGuidesNavigation adds api to navigation.
func SetGuidesNavigation(s *spec.APISpecification, guidesnav []*navigation.Node) {
	id := ""
	if s != nil {
		id = s.ID
	}

	guides[id] = guidesnav
}
