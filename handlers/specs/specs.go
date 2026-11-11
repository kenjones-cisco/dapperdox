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
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/logger"
)

// Register creates routes for each static resource.
func Register(r *mux.Router, discovered map[string][]byte) error {
	log().Info("Registering specifications")

	specReplacer := loadReplacer()

	var specMap map[string][]byte

	if discovered != nil {
		specMap = make(map[string][]byte, len(discovered))
		for key, raw := range discovered {
			specMap["/"+strings.TrimPrefix(key, "/")+"/api.json"] = raw
		}
	} else {
		var err error

		specMap, err = loadSpecsByDir()
		if err != nil {
			return err
		}
	}

	for k := range specMap {
		// capture temporary instance to avoid overwritten values in discovery approach.
		tmpSpec := specMap[k]

		// Replace URLs in document
		tmpSpec = []byte(specReplacer.Replace(string(tmpSpec)))

		r.Path(k).Methods(http.MethodGet).HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-control", "public, max-age=259200")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tmpSpec)
		})
	}

	return nil
}

func loadReplacer() *strings.Replacer {
	// Build a replacer to search/replace specification URLs
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

	return strings.NewReplacer(replacements...)
}

func loadSpecsByDir() (map[string][]byte, error) {
	if viper.GetString(config.SpecDir) == "" {
		log().Info("- No local specifications to serve")

		return map[string][]byte{}, nil
	}

	base, err := filepath.Abs(filepath.Clean(viper.GetString(config.SpecDir)))
	if err != nil {
		return nil, fmt.Errorf("form specification path: %w", err)
	}

	log().Debug(fmt.Sprintf("- Scanning base directory %s", base))

	base = filepath.ToSlash(base)
	specMap := make(map[string][]byte)

	err = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if path == base {
			// Nothing to do with this path
			return nil
		}

		log().Debug(fmt.Sprintf("  - %s", path))

		path = filepath.ToSlash(path)
		ext := filepath.Ext(path)

		switch ext {
		case ".json", ".yml", ".yaml":
			// Strip base path and file extension
			route := strings.TrimPrefix(path, base)

			log().Debug(fmt.Sprintf("    = URL : %s", route))
			log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("    + File: %s", path))

			content, err := os.ReadFile(path) //nolint:gosec // Paths come from the configured specification directory, not request input.
			if err != nil {
				return err
			}

			specMap[route] = content
		}

		return nil
	})

	return specMap, err
}
