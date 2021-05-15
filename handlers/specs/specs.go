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

// Package specs provides handler for API specs.
package specs

import (
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover"
)

var (
	specMap      map[string][]byte
	specReplacer *strings.Replacer
)

// Register creates routes for each static resource.
func Register(r *mux.Router, d discover.DiscoveryManager) {
	log().Info("Registering specifications")

	loadReplacer()

	if viper.GetBool(config.DiscoveryEnabled) {
		specMap = loadSpecsByDiscovery(d)
	} else {
		specMap = loadSpecsByDir()
	}

	for k := range specMap {
		// Replace URLs in document
		specMap[k] = []byte(specReplacer.Replace(string(specMap[k])))

		r.Path(k).Methods(http.MethodGet).HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			serveSpec(w, k)
		})
	}
}

func loadReplacer() {
	// Build a replacer to search/replace specification URLs
	if specReplacer == nil {
		var replacements []string

		// Configure the replacer with key=value pairs
		for k, v := range viper.GetStringMapString(config.SpecRewriteURL) {
			if v != "" {
				// Map between configured to=from URL pair
				replacements = append(replacements, k, v)
			} else {
				// Map between configured URL and site URL
				replacements = append(replacements, k, viper.GetString(config.SiteURL))
			}
		}

		specReplacer = strings.NewReplacer(replacements...)
	}
}

func loadSpecsByDiscovery(d discover.DiscoveryManager) map[string][]byte {
	if d == nil {
		log().Info("- No discovered specificiations to serve")

		return nil
	}

	return d.Specs()
}

func loadSpecsByDir() map[string][]byte {
	if viper.GetString(config.SpecDir) == "" {
		log().Info("- No local specifications to serve")

		return nil
	}

	base, err := filepath.Abs(filepath.Clean(viper.GetString(config.SpecDir)))
	if err != nil {
		log().Errorf("Error forming specification path: %s", err)

		return nil
	}

	log().Debugf("- Scanning base directory %s", base)

	base = filepath.ToSlash(base)
	specMap = make(map[string][]byte)

	_ = filepath.Walk(base, func(path string, _ os.FileInfo, _ error) error {
		if path == base {
			// Nothing to do with this path
			return nil
		}

		log().Debugf("  - %s", path)

		path = filepath.ToSlash(path)
		ext := filepath.Ext(path)

		switch ext {
		case ".json", ".yml", ".yaml":
			// Strip base path and file extension
			route := strings.TrimPrefix(path, base)

			log().Debugf("    = URL : %s", route)
			log().Tracef("    + File: %s", path)

			specMap[route], _ = ioutil.ReadFile(path)
		}

		return nil
	})

	return specMap
}

func serveSpec(w http.ResponseWriter, resource string) {
	log().Debugf("Serve file %s", resource)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-control", "public, max-age=259200")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(specMap[resource])
}
