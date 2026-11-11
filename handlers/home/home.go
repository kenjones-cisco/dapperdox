/*
Copyright (C) 2016-2017 dapperdox.com

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.

*/

// Package home provides handler for homepage.
package home

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/logger"
	"github.com/kenjones-cisco/dapperdox/render"
	"github.com/kenjones-cisco/dapperdox/spec"
)

// Register creates routes for each home handler.
func Register(r *mux.Router, renderer *render.Context) {
	RegisterWithConfig(r, renderer, config.Snapshot())
}

// RegisterWithConfig registers homes using this application's settings.
func RegisterWithConfig(r *mux.Router, renderer *render.Context, settings config.Settings) {
	log().Debug("registering handlers for home page")

	// Homepages for each loaded specification
	var specification *spec.APISpecification // Ends up being populated with the last spec processed

	for _, current := range renderer.Suite.Specifications {
		specification = current

		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Build homepage route for specification %q", specification.ID))

		r.Path("/" + specification.ID + "/reference").Methods(http.MethodGet).HandlerFunc(specificationSummaryHandler(renderer, specification))

		// If missingh trailing slash, redirect to add it
		r.Path("/" + specification.ID).Methods(http.MethodGet).HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/"+current.ID+"/", http.StatusFound)
		})
	}

	if len(renderer.Suite.Specifications) == 1 && !settings.ForceSpecList {
		// If there is only one specification loaded, then hotwire '/' to redirect to the
		// specification summary page unless DapperDox is configured to show the specification list page.
		r.Path("/").Methods(http.MethodGet).HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/"+specification.ID+"/reference", http.StatusFound)
		})
	} else {
		r.Path("/").Methods(http.MethodGet).HandlerFunc(func(w http.ResponseWriter, req *http.Request) { specificationListHandler(renderer, w, req) })
	}
}

func specificationListHandler(renderer *render.Context, w http.ResponseWriter, req *http.Request) {
	log().Log(context.Background(), logger.LevelTrace, "Render HTML for top level index page")

	renderer.HTML(w, http.StatusOK, "specification_list",
		renderer.DefaultVars(req, nil, render.Vars{"Title": "Specifications list", "SpecificationList": true}))
}

func specificationSummaryHandler(renderer *render.Context, s *spec.APISpecification) func(w http.ResponseWriter, req *http.Request) {
	// The default "theme" level reference index page.
	tmpl := "specification_summary"

	customTmpl := s.ID + "/specification_summary"

	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("+ Test for template %q", customTmpl))

	if renderer.TemplateLookup(customTmpl) != nil {
		tmpl = customTmpl
	}

	return func(w http.ResponseWriter, req *http.Request) {
		renderer.HTML(w, http.StatusOK, tmpl,
			renderer.DefaultVars(req, s, render.Vars{"Title": "Specification summary", "SpecificationSummary": true}))
	}
}
