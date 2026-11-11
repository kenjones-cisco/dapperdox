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

package spec

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/logger"
)

var (
	statusMapSplit = regexp.MustCompile(",")
	statusCodes    map[int]string
)

// loadStatusCodes loads status code mappings.
func loadStatusCodes() {
	statusfile := statusCodeFile()

	if statusfile == "" {
		log().Log(context.Background(), logger.LevelTrace, "No status code map file found.")

		return
	}

	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Processing HTTP status code file: %s", statusfile))

	file, err := os.Open(statusfile) //nolint:gosec // This path is selected from operator-configured asset directories.
	if err != nil {
		log().Error(fmt.Sprintf("Error: %s", err))

		return
	}
	defer file.Close()

	statusCodes = make(map[int]string)

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		indexes := statusMapSplit.FindStringIndex(line)
		if indexes == nil {
			return
		}

		i, err := strconv.Atoi(line[0 : indexes[1]-1])
		if err != nil {
			log().Error(fmt.Sprintf("Invalid HTTP status code in csv file: %q", line))

			continue
		}

		statusCodes[i] = line[indexes[1]:]
	}

	if err := scanner.Err(); err != nil {
		log().Error(fmt.Sprintf("Error: %s", err))
	}
}

func statusCodeFile() string {
	var candidates []string

	if assets := viper.GetString(config.AssetsDir); assets != "" {
		candidates = append(candidates, filepath.Join(assets, "status_codes.csv"))
	}

	theme := viper.GetString(config.Theme)
	if themeDir := viper.GetString(config.ThemeDir); themeDir != "" {
		candidates = append(candidates, filepath.Join(themeDir, theme, "status_codes.csv"))
	}

	defaults := viper.GetString(config.DefaultAssetsDir)

	candidates = append(candidates,
		filepath.Join(defaults, "themes", theme, "status_codes.csv"),
		filepath.Join(defaults, "themes", "default", "status_codes.csv"))
	for _, path := range candidates {
		log().Log(context.Background(), logger.LevelTrace, "looking for HTTP status descriptions", slog.String("path", path))

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return path
		}
	}

	return ""
}

func httpStatusDescription(status int) string {
	return statusCodes[status]
}
