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

// Package asset scans a default template directory *and* an override template directory,
// building an "go generate" compliant Assets structure. Templates in under the override
// directory replace of suppliment those in the default directory.
// This allows default themes to be provided, and then changed on a per-use basis by
// dropping files in the override directory.
package asset

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/formatter"
)

var (
	_bindata      = map[string][]byte{}
	_metadata     = map[string]map[string]string{}
	guideReplacer *strings.Replacer
	gfmReplace    []*gfmReplacer
)

var (
	sectionSplitRegex = regexp.MustCompile(`\[\[[\w\-/]+\]\]`)
	gfmMapSplit       = regexp.MustCompile(":")
)

// Asset returns asset content.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.ReplaceAll(name, "\\", "/")
	if a, ok := _bindata[cannonicalName]; ok {
		return a, nil
	}

	return nil, fmt.Errorf("Asset %s not found", name)
}

// Names returns all asset names.
func Names() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}

	return names
}

// MetaData returns file metadata.
func MetaData(filename, name string) string {
	if md, ok := _metadata[filename]; ok {
		if val, ok := md[strings.ToLower(name)]; ok {
			return val
		}
	}

	return ""
}

// Compile specs.
func Compile(dir, prefix string) {
	// Build a replacer to search/replace Document URLs in the documents.
	if guideReplacer == nil {
		var replacements []string

		// Configure the replacer with key=value pairs
		for k, v := range viper.GetStringMapString(config.DocumentRewriteURL) {
			replacements = append(replacements, k, v)
		}

		guideReplacer = strings.NewReplacer(replacements...)
	}

	dir, err := filepath.Abs(dir)
	if err != nil {
		log().Errorf("Error forming absolute path: %s", err)
	}

	log().Debugf("- Scanning directory %s", dir)

	dir = filepath.ToSlash(dir)

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, _ error) error {
		path = filepath.Clean(filepath.ToSlash(path))

		if info == nil {
			return nil
		}
		if info.IsDir() {
			// Skip hidden directories TODO this should be applied to files also.
			_, node := filepath.Split(path)
			if node[0] == '.' {
				return filepath.SkipDir
			}

			return nil
		}

		buf, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}

		relative, err := filepath.Rel(dir, path)
		if err != nil {
			panic(err)
		}

		ext := filepath.Ext(path)

		var meta map[string]string

		switch ext {
		// The file may be in GFM, so convert to HTML and process any embedded metadata
		case ".md":
			// Chop off the extension
			mdname := strings.TrimSuffix(relative, ext)

			buf, meta = processMetadata(buf)

			// This resource may be metadata tagged as a page section overlay..
			if overlay, ok := meta["overlay"]; ok && strings.EqualFold(overlay, "true") {
				// Chop markdown into sections
				sections, headings := splitOnSection(string(buf))

				if sections == nil {
					log().Panicf("  * Error no sections defined in overlay file %s", relative)
				}

				for i, heading := range headings {
					buf = processMarkdown([]byte(sections[i]))

					relative = filepath.Join(mdname, heading, "overlay.tmpl")
					storeTemplate(prefix, relative, guideReplacer.Replace(string(buf)), meta)
				}
			} else {
				buf = processMarkdown(buf) // Convert markdown into HTML

				relative = mdname + ".tmpl"
				storeTemplate(prefix, relative, guideReplacer.Replace(string(buf)), meta)
			}
		case ".tmpl":
			buf, meta = processMetadata(buf)
			storeTemplate(prefix, relative, guideReplacer.Replace(string(buf)), meta)

		case ".html":
			log().Panicf("  * Error - Refusing to process .html files. Expects HTML template fragments with .tmpl extension. File %s", relative)

		default:
			storeTemplate(prefix, relative, guideReplacer.Replace(string(buf)), meta)
		}

		return nil
	})
}

func storeTemplate(prefix, name, template string, meta map[string]string) {
	newname := filepath.ToSlash(filepath.Join(prefix, name))

	if _, ok := _bindata[newname]; !ok {
		log().Debugf("  + Import %s", newname)
		// Store the template, doing and search/replaces on the way
		_bindata[newname] = []byte(template)

		if len(meta) > 0 {
			log().Trace("    + Adding metadata")

			_metadata[newname] = meta
		}
	}
}

// processMarkdown Returns rendered markdown.
func processMarkdown(doc []byte) []byte {
	html := formatter.Markdown(doc)
	// Apply any HTML substitutions
	for _, rep := range gfmReplace {
		html = rep.Regexp.ReplaceAll(html, rep.Replace)
	}

	return html
}

// processMetadata Strips and processed metadata from markdown document.
func processMetadata(doc []byte) ([]byte, map[string]string) {
	// Inspect the markdown src doc to see if it contains metadata
	scanner := bufio.NewScanner(bytes.NewReader(doc))
	scanner.Split(bufio.ScanLines)

	var newdoc string

	metaData := make(map[string]string)

	for scanner.Scan() {
		line := scanner.Text()
		splitLine := strings.Split(line, ":")
		trimmed := strings.TrimSpace(splitLine[0])

		if len(splitLine) < 2 || !unicode.IsLetter(rune(trimmed[0])) { // Have we reached a non KEY: line? If so, we're done with the metadata.
			if len(line) > 0 { // If the line is not empty, keep the contents
				newdoc += line + "\n"
			}

			// Gather up all remainging lines
			for scanner.Scan() {
				// TODO Make this more efficient!
				newdoc += scanner.Text() + "\n"
			}

			break
		}

		// Else, deal with meta-data
		metaValue := ""
		if len(splitLine) > 1 {
			metaValue = strings.TrimSpace(splitLine[1])
		}

		metaData[strings.ToLower(splitLine[0])] = metaValue
	}

	return []byte(newdoc), metaData
}

func splitOnSection(text string) ([]string, []string) {
	indexes := sectionSplitRegex.FindAllStringIndex(text, -1)

	if indexes == nil {
		return nil, nil
	}

	last := 0
	sections := make([]string, len(indexes))
	headings := make([]string, len(indexes))

	for i, element := range indexes {
		if i > 0 {
			sections[i-1] = text[last:element[0]]
		}

		headings[i] = text[element[0]+2 : element[1]-2] // +/- 2 removes the leading/trailing [[ ]]

		last = element[1]
	}

	sections[len(indexes)-1] = text[last:]

	return sections, headings
}

// CompileGFMMap github markdown.
func CompileGFMMap() {
	var mapfile string

	if viper.GetString(config.AssetsDir) != "" {
		mapfile = filepath.Join(viper.GetString(config.AssetsDir), "gfm.map")
		log().Tracef("Looking in assets dir for %s", mapfile)

		if _, err := os.Stat(mapfile); os.IsNotExist(err) {
			mapfile = ""
		}
	}

	if mapfile == "" && viper.GetString(config.ThemeDir) != "" {
		mapfile = filepath.Join(viper.GetString(config.ThemeDir), viper.GetString(config.Theme), "gfm.map")
		log().Tracef("Looking in theme dir for %s", mapfile)

		if _, err := os.Stat(mapfile); os.IsNotExist(err) {
			mapfile = ""
		}
	}

	if mapfile == "" {
		mapfile = filepath.Join(viper.GetString(config.DefaultAssetsDir), "themes", viper.GetString(config.Theme), "gfm.map")
		log().Tracef("Looking in default theme dir for %s", mapfile)

		if _, err := os.Stat(mapfile); os.IsNotExist(err) {
			mapfile = ""
		}
	}

	if mapfile == "" {
		mapfile = filepath.Join(viper.GetString(config.DefaultAssetsDir), "themes", "default", "gfm.map")
		log().Tracef("Looking in default theme for %s", mapfile)

		if _, err := os.Stat(mapfile); os.IsNotExist(err) {
			mapfile = ""
		}
	}

	if mapfile == "" {
		log().Trace("No GFM HTML mapfile found")

		return
	}

	log().Tracef("Processing GFM HTML mapfile: %s", mapfile)

	file, err := os.Open(mapfile)
	if err != nil {
		log().Errorf("Error: %s", err)

		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		rep := &gfmReplacer{}
		if rep.Parse(line) != nil {
			log().Tracef("GFM replace %s with %s", rep.Regexp, rep.Replace)
			gfmReplace = append(gfmReplace, rep)
		}
	}

	if err := scanner.Err(); err != nil {
		log().Errorf("Error: %s", err)
	}
}

type gfmReplacer struct {
	Regexp  *regexp.Regexp
	Replace []byte
}

func (g *gfmReplacer) Parse(line string) *string {
	indexes := gfmMapSplit.FindStringIndex(line)
	if indexes == nil {
		return nil
	}

	g.Regexp = regexp.MustCompile(line[0 : indexes[1]-1])
	g.Replace = []byte(line[indexes[1]:])

	return &line
}
