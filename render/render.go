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

// Package render handles rendering markdown documentation.
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/unrolled/render"

	"github.com/kenjones-cisco/dapperdox/logger"
	"github.com/kenjones-cisco/dapperdox/render/asset"
	"github.com/kenjones-cisco/dapperdox/spec"
)

const (
	sizeTen       = 10
	sizeSixtyFour = 64
)

var (
	errMissingCatalog     = errors.New("renderer requires a suite and asset catalog")
	errMissingLayout      = errors.New("missing layout template")
	errYieldWithoutLayout = errors.New("yield called without layout")
	errMissingTemplate    = errors.New("template not found")
)

// Context owns the rendering state for one immutable documentation generation.
// Set navigation while building the generation, before publishing it.
type Context struct {
	Suite     *spec.Suite
	Assets    *asset.Catalog
	templates *template.Template
	guides    map[string]GuideType
}

// New validates and compiles templates without changing application-global state.
func New(suite *spec.Suite, catalog *asset.Catalog) (*Context, error) {
	if suite == nil || catalog == nil {
		return nil, errMissingCatalog
	}

	c := &Context{Suite: suite, Assets: catalog, guides: make(map[string]GuideType)}
	t := template.New("assets/templates").Delims("[:", ":]").Funcs(c.funcs())

	for _, path := range catalog.Names() {
		if !strings.HasPrefix(path, "assets/templates/") || !strings.HasSuffix(path, ".tmpl") {
			continue
		}

		buf, err := catalog.Asset(path)
		if err != nil {
			return nil, err
		}

		name := strings.TrimSuffix(strings.TrimPrefix(path, "assets/templates/"), ".tmpl")
		if _, err := t.New(name).Parse(string(buf)); err != nil {
			return nil, fmt.Errorf("compile template %s: %w", path, err)
		}
	}

	c.templates = t
	if c.TemplateLookup("layout") == nil {
		return nil, errMissingLayout
	}

	return c, nil
}

// TemplateLookup looks up a template in this generation.
func (c *Context) TemplateLookup(name string) *template.Template {
	if c.templates == nil {
		return nil
	}

	return c.templates.Lookup(name)
}

// HTML renders with request-local functions, sharing the same counter with nested overlays.
// Errors are logged explicitly and incomplete template output is never sent.
func (c *Context) HTML(w io.Writer, status int, name string, binding any, htmlOpt ...render.HTMLOptions) {
	if response, ok := w.(http.ResponseWriter); ok {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "text/html; charset=UTF-8")
	}

	output, err := c.html(name, binding, htmlOpt...)
	if err != nil {
		log().With(slog.Any("error", err)).Error("render HTML")

		if response, ok := w.(http.ResponseWriter); ok {
			http.Error(response, "Unable to render documentation", http.StatusInternalServerError)
		}

		return
	}

	output = injectStatus(output, binding)

	if response, ok := w.(http.ResponseWriter); ok {
		response.WriteHeader(status)
	}

	if _, err := w.Write(output); err != nil {
		log().With(slog.Any("error", err)).Error("write HTML")
	}
}

func (c *Context) funcs() template.FuncMap {
	return template.FuncMap{
		"map":           Map,
		"ext":           Extend,
		"fnn":           FirstNotNil,
		"arr":           Arr,
		"lc":            strings.ToLower,
		"uc":            strings.ToUpper,
		"join":          strings.Join,
		"concat":        func(a, b string) string { return a + b },
		"counter_set":   func(a int) int { return a },
		"counter_add":   func(a int) int { return a },
		"mod":           func(a, m int) int { return a % m },
		"sub":           func(a, b any) int64 { return toInt64(a) - toInt64(b) },
		"safehtml":      func(s string) template.HTML { return template.HTML(s) },
		"haveTemplate":  c.TemplateLookup,
		"overlay":       func(string, ...any) (template.HTML, error) { return "", nil },
		"getAssetPaths": func(s string, d ...any) []string { return getAssetPaths(s, d) },
		"yield":         func() (template.HTML, error) { return "", errYieldWithoutLayout },
		"current":       func() string { return "" },
		"partial":       func(string) (template.HTML, error) { return "", nil },
		"block":         func(string) (template.HTML, error) { return "", nil },
	}
}

func (c *Context) html(name string, binding any, htmlOpt ...render.HTMLOptions) ([]byte, error) {
	t, err := c.templates.Clone()
	if err != nil {
		return nil, err
	}

	counter := 0
	execute := func(name string, data any) (template.HTML, error) {
		var b bytes.Buffer

		execErr := t.ExecuteTemplate(&b, name, data)

		return template.HTML(b.String()), execErr
	}
	funcs := template.FuncMap{
		"counter_set": func(a int) int {
			counter = a

			return counter
		},
		"counter_add": func(a int) int {
			counter += a

			return counter
		},
		"overlay": func(n string, data ...any) (template.HTML, error) { return overlayFunc(t, n, data) },
		"yield":   func() (template.HTML, error) { return execute(name, binding) },
		"current": func() string { return name },
		"partial": func(partial string) (template.HTML, error) {
			target := partial + "-" + name
			if t.Lookup(target) == nil {
				return "", nil
			}

			return execute(target, binding)
		},
	}
	funcs["block"] = funcs["partial"]
	t.Funcs(funcs)

	layout := "layout"
	if len(htmlOpt) > 0 {
		layout = htmlOpt[0].Layout
		t.Funcs(htmlOpt[0].Funcs)
	}

	target := name
	if layout != "" {
		target = layout
	}

	if t.Lookup(name) == nil {
		return nil, fmt.Errorf("%w: %q", errMissingTemplate, name)
	}

	result, err := execute(target, binding)

	return []byte(result), err
}

func overlayFunc(t *template.Template, name string, data []any) (template.HTML, error) {
	if len(data) == 0 || data[0] == nil {
		log().Debug("Data nil")

		return "", nil
	}

	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Overlay: Looking for overlay %s", name))

	datamap, ok := data[0].(map[string]any)
	if !ok {
		log().Log(context.Background(), logger.LevelTrace, "Overlay: type convert of data[0] to map[string]interface{} failed. Not an expected type.")

		return "", nil
	}

	var b bytes.Buffer

	// Look for an overlay file in declaration order.... Highest priority is first.
	for _, op := range overlayPaths(name, datamap) {
		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Overlay: Does %q exist?", op))

		if t.Lookup(op) != nil {
			log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Applying overlay %q", op))

			if err := t.ExecuteTemplate(&b, op, data[0]); err != nil {
				return "", err
			}

			break
		}
	}

	return template.HTML(b.String()), nil
}

func overlayPaths(name string, datamap map[string]any) []string {
	var overlayName []string

	// Use the passed in data structures to determine what type of "page" we are on:
	// 1. API summary page
	// 2. A method/operation page
	// 3. Resource
	// 4. Specification List page
	//
	if _, ok := datamap["API"].(spec.APIGroup); ok {
		if _, ok := datamap["Methods"].([]spec.Method); ok {
			getAPIAssetPaths(name, &overlayName, datamap)
		}

		if _, ok := datamap["Method"].(spec.Method); ok {
			getMethodAssetPaths(name, &overlayName, datamap)
		}
	}

	if _, ok := datamap["Resource"].(*spec.Resource); ok {
		getResourceAssetPaths(name, &overlayName, datamap)
	}

	if _, ok := datamap["SpecificationList"]; ok {
		getSpecificationListPaths(name, &overlayName, datamap)
	}

	if _, ok := datamap["SpecificationSummary"]; ok {
		getSpecificationSummaryPaths(name, &overlayName, datamap)
	}

	return overlayName
}

func getAssetPaths(_ string, data []any) []string {
	if len(data) == 0 {
		return nil
	}

	datamap, _ := data[0].(map[string]any)

	var paths []string

	if _, ok := datamap["API"]; ok {
		if _, ok := datamap["Methods"]; ok {
			// API-group summary page - Shows operations in a group
			getAPIAssetPaths("", &paths, datamap)

			return paths
		}
	}

	if _, ok := datamap["Method"]; ok {
		getMethodAssetPaths("", &paths, datamap) // Method page

		return paths
	}

	if _, ok := datamap["Resource"]; ok {
		getResourceAssetPaths("", &paths, datamap) // Resource page

		return paths
	}

	if _, ok := datamap["SpecificationList"]; ok {
		getSpecificationListPaths("", &paths, datamap) // Specification List page

		return paths
	}

	if _, ok := datamap["SpecificationSummary"]; ok {
		getSpecificationSummaryPaths("", &paths, datamap) // Specification List page

		return paths
	}

	return nil
}

// Some path stem and asset name helper stuff, to allow the path generation code to
// create asset file paths (for author debug), or the imported assets they create (use by
// the overlay handler).
type overlayStems struct {
	specStem   string
	globalStem string
	asset      string
}

func getOverlayStems(overlayAsset string) *overlayStems {
	if overlayAsset != "" {
		return &overlayStems{
			asset: "/" + overlayAsset + "/overlay",
		}
	}

	return &overlayStems{
		specStem:   "assets/sections/",
		globalStem: "assets/templates/",
		asset:      ".md",
	}
}

func getMethodAssetPaths(overlayAsset string, paths *[]string, datamap map[string]any) {
	method, _ := datamap["Method"].(spec.Method)
	apiID := method.APIGroup.ID

	a := getOverlayStems(overlayAsset)

	if specID, ok := datamap["ID"].(string); ok {
		*paths = append(*paths,
			a.specStem+specID+"/templates/reference/"+apiID+"/"+method.ID+a.asset,
			a.specStem+specID+"/templates/reference/"+apiID+"/"+method.Method+a.asset,
			a.specStem+specID+"/templates/reference/"+apiID+"/method"+a.asset,
			a.specStem+specID+"/templates/reference/"+method.ID+a.asset,
			a.specStem+specID+"/templates/reference/"+method.Method+a.asset,
			a.specStem+specID+"/templates/reference/method"+a.asset,
		)
	}

	*paths = append(*paths,
		a.globalStem+"reference/"+method.ID+a.asset,
		a.globalStem+"reference/"+method.Method+a.asset,
		a.globalStem+"reference/method"+a.asset,
	)
}

func getAPIAssetPaths(overlayAsset string, paths *[]string, datamap map[string]any) {
	apiID := datamap["API"].(spec.APIGroup).ID

	a := getOverlayStems(overlayAsset)

	if specID, ok := datamap["ID"].(string); ok {
		*paths = append(*paths,
			a.specStem+specID+"/templates/reference/"+apiID+a.asset,
			a.specStem+specID+"/templates/reference/api"+a.asset,
		)
	}

	*paths = append(*paths, a.globalStem+"reference/api"+a.asset)
}

func getResourceAssetPaths(overlayAsset string, paths *[]string, datamap map[string]any) {
	resID := datamap["Resource"].(*spec.Resource).ID
	a := getOverlayStems(overlayAsset)

	if specID, ok := datamap["ID"].(string); ok {
		*paths = append(*paths,
			a.specStem+specID+"/templates/resource/"+resID+a.asset,
			a.specStem+specID+"/templates/reference/resource"+a.asset,
		)
	}

	*paths = append(*paths, a.globalStem+"resource/resource"+a.asset)
}

func getSpecificationListPaths(overlayAsset string, paths *[]string, _ map[string]any) {
	a := getOverlayStems(overlayAsset)
	*paths = append(*paths, a.globalStem+"reference/specification_list"+a.asset)
}

func getSpecificationSummaryPaths(overlayAsset string, paths *[]string, datamap map[string]any) {
	a := getOverlayStems(overlayAsset)

	if specID, ok := datamap["ID"].(string); ok {
		*paths = append(*paths, a.specStem+specID+"/templates/reference/specification_summary"+a.asset)
	}

	*paths = append(*paths, a.globalStem+"reference/specification_summary"+a.asset)
}

// toInt64 converts integer types to 64-bit integers.
func toInt64(v any) int64 {
	if str, ok := v.(string); ok {
		iv, err := strconv.ParseInt(str, sizeTen, sizeSixtyFour)
		if err != nil {
			return 0
		}

		return iv
	}

	val := reflect.Indirect(reflect.ValueOf(v))

	switch val.Kind() {
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		return val.Int()
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint, reflect.Uint64:
		tv := val.Uint()
		if tv <= math.MaxInt64 {
			return int64(tv)
		}

		// TODO: What is the sensible thing to do here?
		return math.MaxInt64
	case reflect.Float32, reflect.Float64:
		return int64(val.Float())
	case reflect.Bool:
		if val.Bool() {
			return 1
		}

		return 0
	default:
		return 0
	}
}
