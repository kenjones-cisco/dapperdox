package render

import (
	"net/http"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/navigation"
	"github.com/kenjones-cisco/dapperdox/spec"
)

// GuideType defines an array of Navigation for guides.
type GuideType []*navigation.Node

// Vars is a map of variables.
type Vars map[string]interface{}

// DefaultVars adds the default vars (config, specs, others....) to the data map.
func (c *Context) DefaultVars(req *http.Request, s *spec.APISpecification, m Vars) map[string]any {
	if m == nil {
		log().Trace("creating new template data map")

		m = make(map[string]any)
	}

	m["Config"] = config.C
	m["APISuite"] = c.Suite.Specifications
	m["APISuiteGroups"] = c.Suite.Groups

	if req != nil {
		if status, ok := req.Context().Value(statusKey{}).(map[string]Status); ok {
			m["DocumentationStatus"] = status
		}
	}

	// If we have a multiple specifications or are forcing a parent "root" page for the single specification
	// then set MultipleSpecs to true to enable navigation back to the root page.
	if viper.GetBool(config.ForceSpecList) || len(c.Suite.Specifications) > 1 {
		m["MultipleSpecs"] = true
	}

	if s == nil {
		m["NavigationGuides"] = c.guides[""] // Global guides
		m["SpecPath"] = ""

		return m
	}

	// Per specification defaults
	m["NavigationGuides"] = c.guides[s.ID]

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
func (c *Context) SetGuidesNavigation(s *spec.APISpecification, guidesnav []*navigation.Node) {
	id := ""
	if s != nil {
		id = s.ID
	}

	c.guides[id] = guidesnav
}
