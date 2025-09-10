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

// Package guides provide handler for API Explorer/Guides.
package guides

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gorilla/mux"

	"github.com/kenjones-cisco/dapperdox/navigation"
	"github.com/kenjones-cisco/dapperdox/render"
	"github.com/kenjones-cisco/dapperdox/render/asset"
	"github.com/kenjones-cisco/dapperdox/spec"
)

const maxNavLevels = 2

// Register routes for guide pages.
func Register(r *mux.Router) {
	log().Info("Registering guides")

	// specification specific guides
	for _, specification := range spec.APISuite {
		log().Debugf("- Specification guides for %q", specification.APIInfo.Title)
		register(r, "assets/templates", specification)
	}

	// Top level guides
	log().Debug("- Root guides")
	register(r, "assets/templates", nil)
}

func register(r *mux.Router, base string, specification *spec.APISpecification) {
	rootNode := "/guides"
	routeBase := "/guides"

	if specification != nil {
		rootNode = "/" + specification.ID + "/templates" + rootNode
		routeBase = "/" + specification.ID + routeBase
	}

	pathBase := base + rootNode

	guidesNavigation := &navigation.Node{}

	guidesNavigation.Children = make([]*navigation.Node, 0)
	guidesNavigation.ChildMap = make(map[string]*navigation.Node)

	log().Tracef("  - Walk compiled asset tree %s", pathBase)

	for _, path := range asset.Names() {
		if !strings.HasPrefix(path, pathBase) { // Only keep assets we want
			continue
		}

		ext := filepath.Ext(path)

		switch ext {
		case ".tmpl", ".md":
			log().Debugf("    - File %s", path)

			// Convert path/filename to route
			route := routeBase + stripBasepathAndExtension(path, pathBase)
			absresource := stripBasepathAndExtension(path, base)
			resource := strings.TrimPrefix(absresource, "/")

			log().Tracef("      = URL  %s", route)

			buildNavigation(guidesNavigation, path, pathBase, route, ext)

			r.Path(route).Methods(http.MethodGet).HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				sid := "TOP LEVEL"
				if specification != nil {
					sid = specification.ID
				}

				log().Tracef("Fetching guide from %q for spec ID %s", resource, sid)
				render.HTML(w, http.StatusOK, resource, render.DefaultVars(req, specification, render.Vars{"Guide": resource}))
			})
		}
	}

	sortNavigation(guidesNavigation)

	// Register default route for this guide set
	r.Path(routeBase).Methods(http.MethodGet).HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		uri := findFirstGuideURI(guidesNavigation)
		log().Infof("Redirect to %s", uri)
		http.Redirect(w, req, uri, http.StatusFound)
	})

	// Register the guides navigation with the renderer
	render.SetGuidesNavigation(specification, guidesNavigation.Children)
}

func findFirstGuideURI(tree *navigation.Node) string {
	var uri string

	for i := range tree.Children {
		node := tree.Children[i]
		uri = node.URI

		if uri == "" {
			if len(node.Children) > 0 {
				uri = findFirstGuideURI(node)
			}
		}

		if uri != "" {
			break
		}
	}

	return uri
}

func sortNavigation(tree *navigation.Node) {
	for i := range tree.Children {
		node := tree.Children[i]

		if len(node.Children) > 0 {
			sort.Sort(navigation.ByOrder(node.Children))
		}
	}

	sort.Sort(navigation.ByOrder(tree.Children))
}

func stripBasepathAndExtension(name, basepath string) string {
	// Strip base path and file extension
	return strings.TrimSuffix(strings.TrimPrefix(name, basepath), filepath.Ext(name))
}

func buildNavigation(nav *navigation.Node, path, pathBase, route, ext string) {
	log().Tracef("      - Look for metadata asset %s", path)

	// See if guide has been marked up with navigation metadata...
	hierarchy := asset.MetaData(path, "Navigation")
	sortOrder := asset.MetaData(path, "SortOrder")

	if hierarchy != "" {
		log().Tracef("      * Got navigation metadata %s for file %s", hierarchy, path)
	} else {
		// No Meta Data set on guide, so use the directory structure
		hierarchy = strings.TrimPrefix(strings.TrimSuffix(path, ext), pathBase+"/")
		log().Tracef("      * No navigation metadata for %s. Using path", hierarchy)
	}

	// Break hierarchy into bits
	split := strings.Split(hierarchy, "/")
	parts := len(split)

	if parts > maxNavLevels {
		log().Panicf("Error: Guide %q contains too many navigation levels (%d)", hierarchy, parts)
	}

	if sortOrder == "" {
		sortOrder = route
	}

	current := nav.ChildMap
	currentList := &nav.Children

	// Build tree for this navigation item
	for i := range split {
		name := split[i]
		id := strings.ReplaceAll(strings.ToLower(name), " ", "-")
		id = strings.ReplaceAll(id, ".", "-")

		if i < parts-1 {
			// Have we already created this branch node?
			if currentItem, ok := current[id]; !ok {
				// create new branch node
				current[id] = &navigation.Node{
					ID:        id,
					SortOrder: sortOrder,
					Name:      name,
					ChildMap:  make(map[string]*navigation.Node),
					Children:  make([]*navigation.Node, 0),
				}
				*currentList = append(*currentList, current[id])
				log().Tracef("      + Adding %s = %s to branch", id, current[id].Name)
			} else if sortOrder < currentItem.SortOrder { // Update the branch node sort order, if the leaf has a lower sort
				currentItem.SortOrder = sortOrder
			}

			// Step down branch
			currentList = &current[id].Children // Get parent list before stepping into child
			current = current[id].ChildMap
		} else {
			// Leaf node
			if currentItem, ok := current[id]; !ok {
				current[id] = &navigation.Node{
					ID:        id,
					SortOrder: sortOrder,
					URI:       route,
					Name:      name,
					ChildMap:  make(map[string]*navigation.Node),
					Children:  make([]*navigation.Node, 0),
				}

				*currentList = append(*currentList, current[id])

				log().Tracef("      + Adding %s = %s to leaf node [a] Sort %s", current[id].URI, current[id].Name, sortOrder)
			} else {
				// The page is a leaf node, but sits at a branch node. This means that the branch
				// node has content! Set the uri, and adjust the sort order, if necessary.
				currentItem.URI = route
				if sortOrder < currentItem.SortOrder {
					currentItem.SortOrder = sortOrder
				}

				log().Tracef("      + Adding %s = %s to leaf node [b] Sort %s", currentItem.URI, currentItem.Name, sortOrder)
			}
		}
	}
}
