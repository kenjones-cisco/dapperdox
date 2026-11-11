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

// Package static provides handler for static resources.
package static

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"

	"github.com/kenjones-cisco/dapperdox/render"
)

// Register creates routes for each static resource.
func Register(r *mux.Router, renderer *render.Context) {
	log().Debug("registering not found handler in static package")

	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		renderer.HTML(w, http.StatusNotFound, "error", renderer.DefaultVars(req, nil, map[string]any{"error": "Page not found", "code": http.StatusNotFound}))
	})

	log().Debug("registering static content handlers for static package")

	var allow bool

	for _, file := range renderer.Assets.Names() {
		mimeType := mime.TypeByExtension(filepath.Ext(file))

		if mimeType == "" {
			continue
		}

		log().Debug(fmt.Sprintf("Got MIME type: %s", mimeType))

		switch {
		case strings.HasPrefix(mimeType, "image"),
			strings.HasPrefix(mimeType, "text/css"),
			strings.HasSuffix(mimeType, "javascript"):
			allow = true
		default:
			allow = false
		}

		if allow {
			// Drop assets/static prefix
			path := strings.TrimPrefix(file, "assets/static")

			log().Debug(fmt.Sprintf("registering handler for static asset: %s", path))

			r.Path(path).Methods(http.MethodGet).HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if b, err := renderer.Assets.Asset("assets/static" + path); err == nil {
					w.Header().Set("Content-Type", mimeType)
					w.Header().Set("Cache-control", "public, max-age=259200")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(b)

					return
				}
				// This should never happen!
				log().ErrorContext(req.Context(), fmt.Sprintf("it happened ¯\\_(ツ)_/¯ %s", path))
				r.NotFoundHandler.ServeHTTP(w, req)
			})
		}
	}
}
