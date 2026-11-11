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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (p *Parser) loadStatusCodes() error {
	p.statusCodes = make(map[int]string)

	var candidates []string
	if p.settings.AssetsDir != "" {
		candidates = append(candidates, filepath.Join(p.settings.AssetsDir, "status_codes.csv"))
	}

	if p.settings.ThemeDir != "" {
		candidates = append(candidates, filepath.Join(p.settings.ThemeDir, p.settings.Theme, "status_codes.csv"))
	}

	candidates = append(candidates,
		filepath.Join(p.settings.DefaultAssetsDir, "themes", p.settings.Theme, "status_codes.csv"),
		filepath.Join(p.settings.DefaultAssetsDir, "themes", "default", "status_codes.csv"))
	for _, path := range candidates {
		file, err := os.Open(path) //nolint:gosec // Paths come from operator-configured asset directories.
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			return fmt.Errorf("open status descriptions: %w", err)
		}

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			key, description, found := strings.Cut(scanner.Text(), ",")
			if !found {
				break
			}

			code, err := strconv.Atoi(key)
			if err != nil {
				_ = file.Close()

				return fmt.Errorf("invalid status code %q: %w", key, err)
			}

			p.statusCodes[code] = description
		}

		scanErr, closeErr := scanner.Err(), file.Close()
		if scanErr != nil {
			return fmt.Errorf("read status descriptions: %w", scanErr)
		}

		return closeErr
	}

	return nil
}
