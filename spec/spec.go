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

// Package spec provides API spec loading and parsing.
package spec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-openapi/loads"
	"github.com/go-openapi/spec"
	"github.com/go-openapi/swag/loading"
	"github.com/serenize/snaker"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover"
	"github.com/kenjones-cisco/dapperdox/formatter"
	"github.com/kenjones-cisco/dapperdox/logger"
)

const (
	collectionCSV  = "csv"
	parameterPath  = "path"
	arrayType      = "array"
	objectType     = "object"
	mapType        = "map"
	sortMethod     = "method"
	sortOperation  = "operation"
	sortNavigation = "navigation"
	sortSummary    = "summary"

	excludeOpExt     = "x-excludeFromOperations"
	navMethodNameExt = "x-navigateMethodsByName"
	opNameExt        = "x-operationName"
	pathNameExt      = "x-pathName"
	sortMethodsByExt = "x-sortMethodsBy"
	groupByExt       = "x-groupby"
	versionExt       = "x-version"
	visibilityExt    = "x-visibility"
)

var errMissingDiscovery = errors.New("no discovery provided to fetch specs")

// all defined ResourceOrigin.
const (
	RequestBody ResourceOrigin = iota
	MethodResponse
)

var kababExclude = regexp.MustCompile(`[^\w\s]`) // Any non word or space character

var collectionTable = map[string]string{
	collectionCSV: "comma separated",
	"ssv":         "space separated",
	"tsv":         "tab separated",
	"pipes":       "pipe separated",
	"multi":       "multiple occurrences",
}

var sortTypes = map[string]bool{
	parameterPath:  true,
	sortMethod:     true,
	sortOperation:  true,
	sortNavigation: true,
	sortSummary:    true,
}

// APISpecification holds the content of a parsed api.
type APISpecification struct {
	parser              *Parser
	SecurityDefinitions map[string]SecurityScheme
	DefaultSecurity     map[string]Security
	ResourceList        map[string]map[string]*Resource // Version->ResourceName->Resource
	APIVersions         map[string]APISet               // Version->APISet
	APIInfo             Info
	ID                  string
	URL                 string
	GroupBy             string
	APIs                APISet // APIs represents the parsed APIs
}

// APISet list of grouped APIs.
type APISet []APIGroup

// APIGroup parents all grouped API methods (Grouping controlled by tagging, if used, or by method path otherwise).
type APIGroup struct {
	URL                    *url.URL
	Versions               map[string][]Method // All versions, keyed by version string.
	Info                   *Info
	ID                     string
	Name                   string
	CurrentVersion         string // The latest version in operation for the API
	MethodSortBy           []string
	Methods                []Method // The current version
	Consumes               []string
	Produces               []string
	MethodNavigationByName bool
}

// Info holds display information about API.
type Info struct {
	Title       string
	Description string
}

// Version holds version to list of associated method.
type Version struct {
	Version string
	Methods []Method
}

// OAuth2Scheme is a specific security scheme.
type OAuth2Scheme struct {
	Scopes           map[string]string
	OAuth2Flow       string
	AuthorizationURL string
	TokenURL         string
}

// SecurityScheme holds the security scheme from a parsed api.
type SecurityScheme struct {
	OAuth2Scheme

	Type          string
	Description   string
	ParamName     string
	ParamLocation string
	IsAPIKey      bool
	IsBasic       bool
	IsOAuth2      bool
}

// Security holds the defined enabled security.
type Security struct {
	Scheme *SecurityScheme
	Scopes map[string]string
}

// Method represents an API method.
type Method struct {
	BodyParam       *Parameter
	APIGroup        *APIGroup
	Security        map[string]Security
	DefaultResponse *Response // A ptr to allow of easy checking of its existence in templates
	Responses       map[int]Response
	NavigationName  string
	Method          string
	SortKey         string
	Name            string
	Description     string
	Path            string
	OperationName   string
	ID              string
	FormParams      []Parameter
	HeaderParams    []Parameter
	QueryParams     []Parameter
	Resources       []*Resource
	PathParams      []Parameter
	Produces        []string
	Consumes        []string
}

// Parameter represents an API method parameter.
type Parameter struct {
	Resource                    *Resource // For "in body" parameters
	Name                        string
	Description                 string
	In                          string
	CollectionFormat            string
	CollectionFormatDescription string
	Type                        []string
	Enum                        []string
	Required                    bool
	IsArray                     bool // "in body" parameter is an array
}

// Response represents an API method response.
type Response struct {
	Description       string
	StatusDescription string
	Resource          *Resource
	Headers           []Header
	IsArray           bool
}

// ResourceOrigin defines different resource origin types.
type ResourceOrigin int

// Resource represents an API resource.
type Resource struct {
	Properties            map[string]*Resource
	Methods               map[string]*Method
	Title                 string
	Description           string
	Example               string
	Schema                string
	ID                    string
	Type                  []string // Will contain two elements if an array or map [0]=array [1]=What type is in the array
	ExcludeFromOperations []string
	FQNS                  []string
	Enum                  []string
	origin                ResourceOrigin
	Required              bool
	ReadOnly              bool
}

// Header represents an API parameter.
type Header struct {
	Name                        string
	Description                 string
	CollectionFormat            string
	CollectionFormatDescription string
	Default                     string
	Type                        []string // Will contain two elements if an array [0]=array [1]=What type is in the array
	Enum                        []string
	Required                    bool
}

// SortMethods implements sortable array of method.
type SortMethods []Method

func (a SortMethods) Len() int           { return len(a) }
func (a SortMethods) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a SortMethods) Less(i, j int) bool { return a[i].SortKey < a[j].SortKey }

// LoadSpecifications validates the provided API specifications without publishing globals.
//
// Deprecated: use Parser.LoadSuite or Parser.Parse to obtain parsed specifications.
func LoadSpecifications(d discover.DiscoveryManager) (bool, error) {
	p, err := NewParser(config.Snapshot())
	if err != nil {
		return false, err
	}

	var docs map[string]*loads.Document

	if p.settings.DiscoveryEnabled {
		docs, err = getDocsByDiscovery(d)
	} else {
		docs, err = p.getDocsByDir()
	}

	if err != nil {
		return false, err
	}

	for specLocation, doc := range docs {
		if _, err := p.parseDocument(specLocation, doc); err != nil {
			return false, err
		}
	}

	log().Info(fmt.Sprintf("validated [%d] specifications", len(docs)))

	return len(docs) > 0, nil
}

func getDocsByDiscovery(d discover.DiscoveryManager) (map[string]*loads.Document, error) {
	if d == nil {
		return nil, errMissingDiscovery
	}

	docs := make(map[string]*loads.Document)

	rawspecs := d.Specs()

	for k, data := range rawspecs {
		document, err := loads.Analyzed(json.RawMessage(data), "")
		if err != nil {
			return nil, err
		}

		document, err = document.Expanded()
		if err != nil {
			return nil, err
		}

		log().Debug(fmt.Sprintf("  registering spec[%s]", fmt.Sprintf("%s:%s", k, document.Spec().Info.Version)))
		docs[fmt.Sprintf("/%s/api.json", k)] = document
	}

	return docs, nil
}

func (p *Parser) getDocsByDir() (map[string]*loads.Document, error) {
	log().Info(fmt.Sprintf("configured spec filenames: %v", p.settings.SpecFilename))

	docs := make(map[string]*loads.Document)

	for _, specLocation := range p.settings.SpecFilename {
		log().Info(fmt.Sprintf("specLocation: %s", specLocation))

		document, err := p.loadSpec(p.normalizeSpecLocation(specLocation))
		if err != nil {
			return nil, err
		}

		if isLocalSpecURL(specLocation) && !strings.HasPrefix(specLocation, "/") {
			specLocation = "/" + specLocation
		}

		docs[specLocation] = document
	}

	return docs, nil
}

func (c *APISpecification) load(specLocation string, document *loads.Document) {
	apispec := document.Spec()

	c.URL = specLocation

	basePath := apispec.BasePath
	if basePath == "/" {
		basePath = ""
	}

	scheme := "http"
	if len(apispec.Schemes) > 0 {
		scheme = apispec.Schemes[0]
	}

	host := apispec.Host
	if host == "" {
		host = c.parser.settings.SpecDefaultHost
	}

	u := &url.URL{
		Scheme: scheme,
		Host:   host,
	}

	c.APIInfo.Description = string(formatter.Markdown([]byte(apispec.Info.Description)))
	c.APIInfo.Title = apispec.Info.Title

	if c.APIInfo.Title == "" {
		panic(parseError("specification requires info.title"))
	}

	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Parse OpenAPI specification %q", c.APIInfo.Title))

	c.ID = titleToKebab(c.APIInfo.Title)

	c.getSecurityDefinitions(apispec)
	c.getDefaultSecurity(apispec)

	c.GroupBy = "default"
	if groupBy, ok := apispec.Extensions[groupByExt].(string); ok {
		c.GroupBy = groupBy
	}

	// Should methods in the navigation be presented by type (GET, POST) or name (string)?
	methodNavByName := false
	if byname, ok := apispec.Extensions[navMethodNameExt].(bool); ok {
		methodNavByName = byname
	}

	methodSortBy := methodSortKeys(apispec.Extensions)

	prototype := APIGroup{
		URL: u, Info: &c.APIInfo, MethodNavigationByName: methodNavByName,
		MethodSortBy: methodSortBy, Consumes: apispec.Consumes, Produces: apispec.Produces,
	}
	for _, tag := range getTags(apispec) {
		c.loadTag(document, tag, basePath, prototype)
	}

	// Build a API map, grouping by version
	for i := range c.APIs {
		api := &c.APIs[i]
		for v := range api.Versions {
			if c.APIVersions == nil {
				c.APIVersions = make(map[string]APISet)
			}
			// Create copy of API and set Methods array to be correct for the version we are building
			napi := *api
			napi.Methods = napi.Versions[v]
			napi.Versions = nil
			c.APIVersions[v] = append(c.APIVersions[v], napi) // Group APIs by version
		}
	}
}

func methodSortKeys(extensions spec.Extensions) []string {
	var keys []string

	values, _ := extensions[sortMethodsByExt].([]any)
	for _, value := range values {
		key, _ := value.(string)
		if _, ok := sortTypes[key]; !ok {
			log().Error("invalid x-sortBy value", slog.String("key", key))
		} else {
			keys = append(keys, key)
		}
	}

	return keys
}

func (c *APISpecification) loadTag(document *loads.Document, tag spec.Tag, basePath string, prototype APIGroup) {
	name := tag.Description
	if name == "" {
		name = tag.Name
	}

	prototype.ID, prototype.Name = titleToKebab(name), name
	api := new(APIGroup)
	*api = prototype
	groupingByTag := tag.Name != ""
	paths := document.Analyzer.AllPaths()

	for path := range paths {
		pathItem := paths[path]
		if isPrivate(pathItem.Extensions) {
			continue
		}

		path = basePath + path

		if !groupingByTag {
			api = new(APIGroup)
			*api = prototype
		}

		ver, ok := pathItem.Extensions[versionExt].(string)
		if !ok {
			ver = "latest"
		}

		api.CurrentVersion = ver
		c.getMethods(tag, api, &api.Methods, &pathItem, path, ver)

		if !groupingByTag {
			c.appendAPI(api)
		}
	}

	if groupingByTag {
		c.appendAPI(api)
	}
}

func (c *APISpecification) appendAPI(api *APIGroup) {
	if len(api.Methods) == 0 {
		return
	}

	sort.Sort(SortMethods(api.Methods))
	c.APIs = append(c.APIs, *api)
}

func (c *APISpecification) getMethods(tag spec.Tag, api *APIGroup, methods *[]Method, pi *spec.PathItem, path, version string) {
	c.getMethod(tag, api, methods, version, pi, pi.Get, path, "get")
	c.getMethod(tag, api, methods, version, pi, pi.Post, path, "post")
	c.getMethod(tag, api, methods, version, pi, pi.Put, path, "put")
	c.getMethod(tag, api, methods, version, pi, pi.Delete, path, "delete")
	c.getMethod(tag, api, methods, version, pi, pi.Head, path, "head")
	c.getMethod(tag, api, methods, version, pi, pi.Options, path, "options")
	c.getMethod(tag, api, methods, version, pi, pi.Patch, path, "patch")
}

func (c *APISpecification) getMethod(tag spec.Tag, api *APIGroup, methods *[]Method, version string, pathitem *spec.PathItem, operation *spec.Operation, path, methodname string) {
	if operation == nil {
		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Skipping %s %s - Operation is nil.", path, methodname))

		return
	}

	if isPrivate(operation.Extensions) {
		log().Debug(fmt.Sprintf("Skipping %s %s - Operation is private", path, methodname))

		return
	}

	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("  Operation tag length: %d", len(operation.Tags)))

	// Filter and sort by matching current top-level tag with the operation tags.
	// If Tagging is not used by spec, then process each operation without filtering.
	if len(operation.Tags) == 0 {
		if tag.Name != "" {
			log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Skipping %s - Operation does not contain a tag member, and tagging is in use.", operation.Summary))

			return
		}

		method := c.processMethod(api, pathitem, operation, path, methodname, version)
		*methods = append(*methods, *method)
	} else {
		log().Log(context.Background(), logger.LevelTrace, "    > Check tags")

		for _, t := range operation.Tags {
			log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("      - Compare tag %q with %q", tag.Name, t))

			if tag.Name == "" || t == tag.Name {
				method := c.processMethod(api, pathitem, operation, path, methodname, version)
				*methods = append(*methods, *method)
			}
		}
	}
}

func (c *APISpecification) getSecurityDefinitions(s *spec.Swagger) {
	if c.SecurityDefinitions == nil {
		c.SecurityDefinitions = make(map[string]SecurityScheme)
	}

	for n, d := range s.SecurityDefinitions {
		stype := d.Type

		def := &SecurityScheme{
			Description:   string(formatter.Markdown([]byte(d.Description))),
			Type:          stype,  // basic, apiKey or oauth2
			ParamName:     d.Name, // name of header to be used if ParamLocation is 'header'
			ParamLocation: d.In,   // Either query or header
		}

		if stype == "apiKey" {
			def.IsAPIKey = true
		}

		if stype == "basic" {
			def.IsBasic = true
		}

		if stype == "oauth2" {
			def.IsOAuth2 = true
			def.OAuth2Flow = d.Flow                   // implicit, password (explicit) application or accessCode
			def.AuthorizationURL = d.AuthorizationURL // Only for implicit or accesscode flow
			def.TokenURL = d.TokenURL                 // Only for implicit, accesscode or password flow
			def.Scopes = make(map[string]string)

			maps.Copy(def.Scopes, d.Scopes)
		}

		c.SecurityDefinitions[n] = *def
	}
}

func (c *APISpecification) getDefaultSecurity(s *spec.Swagger) {
	c.DefaultSecurity = make(map[string]Security)
	c.processSecurity(s.Security, c.DefaultSecurity)
}

func (c *APISpecification) processMethod(api *APIGroup, pathItem *spec.PathItem, o *spec.Operation, path, methodname, version string) *Method {
	id, operationName := methodIdentity(o, methodname)

	navigationName := operationName
	if api.MethodNavigationByName {
		navigationName = o.Summary
	}

	sortkey := api.getMethodSortKey(path, methodname, operationName, navigationName, o.Summary)

	method := &Method{
		ID:             camelToKebab(id),
		Name:           o.Summary,
		Description:    string(formatter.Markdown([]byte(o.Description))),
		Method:         methodname,
		Path:           path,
		Responses:      make(map[int]Response),
		NavigationName: navigationName,
		OperationName:  operationName,
		APIGroup:       api,
		SortKey:        sortkey,
	}

	c.populateMethod(api, pathItem, o, method, version)

	return method
}

func methodIdentity(o *spec.Operation, methodname string) (id, operationName string) {
	var (
		opname    string
		gotOpname bool
	)

	operationName = methodname
	if opname, gotOpname = o.Extensions[opNameExt].(string); gotOpname {
		operationName = opname
	}

	// Construct an ID for the Method. Choose from operation ID, x-operationName, summary and lastly method name.
	id = o.ID // OperationID
	if id == "" {
		// No ID, use x-operationName, if we have it...
		if gotOpname {
			id = titleToKebab(opname)
		} else {
			id = titleToKebab(o.Summary) // No opname, use summary
			if id == "" {
				id = methodname // Last chance. Method name.
			}
		}
	}

	return id, operationName
}

func (c *APISpecification) populateMethod(api *APIGroup, pathItem *spec.PathItem, o *spec.Operation, method *Method, version string) {
	if len(o.Consumes) > 0 {
		method.Consumes = o.Consumes
	} else {
		method.Consumes = api.Consumes
	}

	if len(o.Produces) > 0 {
		method.Produces = o.Produces
	} else {
		method.Produces = api.Produces
	}

	// If Tagging is not used by spec to select, group and order API paths to document, then
	// complete the missing names.
	// First try the vendor extension x-pathName, falling back to summary if not set.
	// Note, that the APIGroup will get the last pathName set on the path methods added to the group (by tag).
	//
	if pathname, ok := pathItem.Extensions[pathNameExt].(string); ok {
		api.Name = pathname
		api.ID = titleToKebab(api.Name)
	}

	if api.Name == "" {
		name := o.Summary
		if name == "" {
			panic(parseError(fmt.Sprintf("operation %q requires operationId or summary", method.ID)))
		}

		api.Name = name
		api.ID = titleToKebab(name)
	}

	if c.ResourceList == nil {
		c.ResourceList = make(map[string]map[string]*Resource)
	}

	c.processParameters(pathItem.Parameters, method, version)

	c.processParameters(o.Parameters, method, version)

	// Compile resources from response declaration
	if o.Responses == nil {
		panic(parseError(fmt.Sprintf("operation %s %s requires responses", method.Method, method.Path)))
	}
	// FIXME - Dies if there are no responses...
	for status, response := range o.Responses.StatusCodeResponses {
		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Response for status %d", status))

		// Discover if the resource is already declared, keyed on version number.
		if response.Schema != nil {
			if _, ok := c.ResourceList[version]; !ok {
				c.ResourceList[version] = make(map[string]*Resource)
			}
		}

		rsp := c.buildResponse(&response, method, version)
		rsp.StatusDescription = c.parser.statusCodes[status]
		method.Responses[status] = *rsp
	}

	if o.Responses.Default != nil {
		rsp := c.buildResponse(o.Responses.Default, method, version)
		method.DefaultResponse = rsp
	}

	// If no Security given for operation, then the global defaults are appled.
	method.Security = make(map[string]Security)
	if !c.processSecurity(o.Security, method.Security) {
		method.Security = c.DefaultSecurity
	}
}

func (c *APISpecification) processParameters(params []spec.Parameter, method *Method, version string) {
	for i := range params {
		param := &params[i]
		p := Parameter{
			Name:        param.Name,
			In:          param.In,
			Description: string(formatter.Markdown([]byte(param.Description))),
			Required:    param.Required,
		}
		p.setType(*param)
		p.setEnums(*param)

		switch strings.ToLower(param.In) {
		case "formdata":
			method.FormParams = append(method.FormParams, p)
		case parameterPath:
			method.PathParams = append(method.PathParams, p)
		case "body":
			if param.Schema == nil {
				panic(parseError(fmt.Sprintf("body parameter %q requires schema", param.Name)))
			}

			var body map[string]any

			p.Resource, body, p.IsArray = c.resourceFromSchema(param.Schema, method, nil, true)
			p.Resource.Schema = jsonResourceToString(body, p.IsArray)
			p.Resource.origin = RequestBody
			method.BodyParam = &p
			c.crossLinkMethodAndResource(p.Resource, method, version)
		case "header":
			method.HeaderParams = append(method.HeaderParams, p)
		case "query":
			method.QueryParams = append(method.QueryParams, p)
		}
	}
}

func (c *APISpecification) buildResponse(resp *spec.Response, method *Method, version string) *Response {
	var response *Response

	if resp != nil {
		var (
			vres        *Resource
			r           *Resource
			isArray     bool
			exampleJSON map[string]any
		)

		if resp.Schema != nil {
			r, exampleJSON, isArray = c.resourceFromSchema(resp.Schema, method, nil, false)

			if r != nil {
				r.Schema = jsonResourceToString(exampleJSON, false)
				r.origin = MethodResponse
				vres = c.crossLinkMethodAndResource(r, method, version)
			}
		}

		response = &Response{
			Description: string(formatter.Markdown([]byte(resp.Description))),
			Resource:    vres,
			IsArray:     isArray,
		}
		method.Resources = append(method.Resources, response.Resource) // Add the resource to the method which uses it

		response.compileHeaders(resp)
	}

	return response
}

func (c *APISpecification) crossLinkMethodAndResource(resource *Resource, method *Method, version string) *Resource {
	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("++ Resource version %s  ID %s", version, resource.ID))

	if _, ok := c.ResourceList[version]; !ok {
		c.ResourceList[version] = make(map[string]*Resource)
	}

	// Look for a pre-declared resource with the response ID, and use that or create the first one...
	var (
		resFound bool
		vres     *Resource
	)

	if vres, resFound = c.ResourceList[version][resource.ID]; !resFound {
		log().Log(context.Background(), logger.LevelTrace, "   - Creating new resource")

		vres = resource
	}

	// Add to the compiled list of methods which use this resource.
	if vres.Methods == nil {
		vres.Methods = make(map[string]*Method)
	}

	vres.Methods[method.ID] = method // Use a map to collapse duplicates.

	// Store resource in resource-list of the specification, considering precident.
	if resource.origin == RequestBody {
		// Resource is a Request Body - the lowest precident
		log().Log(context.Background(), logger.LevelTrace, "   - Resource origin is a request body")

		// If this is the first time the resource has been seen, it's okay to store this in
		// the global list. A request body resource is a filtered (excludes read-only) resource,
		// and has a lower precident than a response resource.
		if !resFound {
			log().Log(context.Background(), logger.LevelTrace, "     - Not seen before, so storing in global list")

			c.ResourceList[version][resource.ID] = vres
		}
	} else {
		log().Log(context.Background(), logger.LevelTrace, "   - Resource origin is a response, so storing in global list")

		// This is a response resource (which has the highest precident). If an existing
		// request-body resource was found in the cache, then it is replaced by the
		// response resource (but maintaining the method list associated with the resource).
		if resFound && vres.origin == RequestBody {
			resource.Methods = vres.Methods
			vres = resource
		}

		c.ResourceList[version][resource.ID] = vres // If we've already got the resource, this does nothing
	}

	return vres
}

func (c *APISpecification) processSecurity(s []map[string][]string, security map[string]Security) bool {
	count := 0

	for _, sec := range s {
		for n, scopes := range sec {
			// Lookup security name in definitions
			if scheme, ok := c.SecurityDefinitions[n]; ok {
				count++

				// Add security
				security[scheme.Type] = Security{
					Scheme: &scheme,
					Scopes: make(map[string]string),
				}

				populateSecurityScopes(scheme, scopes, security[scheme.Type].Scopes)
			}
		}
	}

	return count != 0
}

func populateSecurityScopes(scheme SecurityScheme, scopes []string, result map[string]string) {
	if !scheme.IsOAuth2 {
		return
	}

	for _, scope := range scopes {
		if description, ok := scheme.Scopes[scope]; ok {
			result[scope] = description
		}
	}
}

func (c *APISpecification) resourceFromSchema(s *spec.Schema, method *Method, fqNS []string, isRequestResource bool) (*Resource, map[string]any, bool) {
	if s == nil {
		return nil, nil, false
	}

	log().Log(context.Background(), logger.LevelTrace, "parse resource schema",
		slog.String("type", checkPropertyType(s)), slog.Any("namespace", fqNS))

	// It is possible for a response to be an array of
	//     objects, and it possible to declare this in several ways:
	// 1. As :
	//      "schema": {
	//        "$ref": "model"
	//      }
	//      Where the model declares itself of type array (of objects)
	// 2. Or :
	//    "schema": {
	//        "type": "array",
	//        "items": {
	//            "$ref": "model"
	//        }
	//    }
	//
	//  In the second version, "items" points to a schema. So what we have done to align these
	//  two cases is to keep the top level "type" in the second case, and apply it to items.schema.Type,
	//  reseting our schema variable to items.schema.

	originalS := s
	s = remapResourceSchema(s)

	if s.Format != "" {
		s.Type[len(s.Type)-1] = s.Format
	}

	id := titleToKebab(s.Title)

	if len(fqNS) == 0 && id == "" {
		panic(parseError(fmt.Sprintf("model for %s %s requires title", method.Method, method.Path)))
	}

	// Ignore ID (from title element) for all but child-objects...
	// This prevents the title-derived ID being added onto the end of the FQNS.property as
	// FQNS.property.ID, if title is given for the property in the spec.
	if len(fqNS) > 0 && !s.Type.Contains(objectType) {
		id = ""
	}

	var isArray bool

	if strings.EqualFold(s.Type[0], arrayType) {
		fqNSlen := len(fqNS)
		if fqNSlen > 0 {
			fqNS = append(fqNS[0:fqNSlen-1], fqNS[fqNSlen-1]+"[]")
		}

		isArray = true
	}

	myFQNS := fqNS

	var chopped bool

	if id == "" && len(myFQNS) > 0 {
		id = myFQNS[len(myFQNS)-1]
		myFQNS = append([]string{}, myFQNS[0:len(myFQNS)-1]...)
		chopped = true

		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Chopped %s from myFQNS leaving %s", id, myFQNS))
	}

	resourceFQNS := myFQNS
	// If we are dealing with an object, then adjust the resource FQNS and id
	// so that the last element of the FQNS is chopped off and used as the ID
	if !chopped && s.Type.Contains(objectType) && len(resourceFQNS) > 0 {
		id = resourceFQNS[len(resourceFQNS)-1]
		resourceFQNS = resourceFQNS[:len(resourceFQNS)-1]

		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Got an object, so slicing %s from resourceFQNS leaving %s", id, myFQNS))
	}

	// If there is no description... the case where we have an array of objects. See issue/11
	description := resourceDescription(originalS)

	r := &Resource{
		ID:          id,
		Title:       s.Title,
		Description: description,
		Type:        s.Type,
		Properties:  make(map[string]*Resource),
		FQNS:        resourceFQNS,
	}

	populateResourceMetadata(r, s, originalS, isRequestResource)

	required := make(map[string]bool)
	jsonRepresentation := make(map[string]any)

	c.compileproperties(s, r, method, id, required, jsonRepresentation, myFQNS, chopped, isRequestResource)

	for allof := range s.AllOf {
		c.compileproperties(&s.AllOf[allof], r, method, id, required, jsonRepresentation, myFQNS, chopped, isRequestResource)
	}

	return r, jsonRepresentation, isArray
}

func resourceDescription(s *spec.Schema) string {
	if s.Description != "" {
		return string(formatter.Markdown([]byte(s.Description)))
	}

	return s.Title
}

func remapResourceSchema(s *spec.Schema) *spec.Schema {
	if len(s.Type) == 0 {
		s.Type = append(s.Type, objectType)
	}

	if s.Items == nil {
		return s
	}

	originalType := s.Type
	switch {
	case s.Items.Schema != nil:
		s = s.Items.Schema
	case len(s.Items.Schemas) > 0:
		s = &s.Items.Schemas[0]
	default:
		panic(parseError("array items require a schema"))
	}

	if len(s.Type) > 0 && !s.Type.Contains(arrayType) && originalType.Contains(arrayType) && len(s.Properties) == 0 {
		s.Type = spec.StringOrArray{arrayType, s.Type[0]}
	} else {
		s.Type = originalType
	}

	return s
}

func populateResourceMetadata(r *Resource, s, original *spec.Schema, isRequestResource bool) {
	if s.Example != nil {
		example, err := jsonMarshalIndent(&s.Example)
		if err != nil {
			log().Error(fmt.Sprintf("Error encoding example json: %s", err))
		}

		r.Example = string(example)
	}

	if len(s.Enum) > 0 {
		for _, e := range s.Enum {
			r.Enum = append(r.Enum, fmt.Sprintf("%s", e))
		}
	}

	r.ReadOnly = original.ReadOnly

	if ops, ok := original.Extensions[excludeOpExt].([]any); ok && isRequestResource {
		// Mark resource property as being excluded from operations with this name.
		// This filtering only takes effect in a request body, just like readOnly, so when isRequestResource is true
		for _, op := range ops {
			if c, ok := op.(string); ok {
				r.ExcludeFromOperations = append(r.ExcludeFromOperations, c)
			}
		}
	}
}

// Takes a Schema object and adds properties to the Resource object.
// It uses the 'required' map to set when properties are required and builds a JSON
// representation of the resource.
func (c *APISpecification) compileproperties(s *spec.Schema, r *Resource, method *Method, id string, required map[string]bool, jsonRep map[string]any, myFQNS []string,
	chopped, isRequestResource bool,
) {
	// First, grab the required members
	for _, n := range s.Required {
		required[n] = true
	}

	for name := range s.Properties {
		property := s.Properties[name]
		c.processProperty(&property, name, r, method, id, required, jsonRep, myFQNS, chopped, isRequestResource)
	}

	// Special case to deal with AdditionalProperties (which really just boils down to declaring a
	// map of 'type' (string, int, object etc).
	if s.AdditionalProperties != nil && s.AdditionalProperties.Allows {
		name := "<key>"
		ap := s.AdditionalProperties.Schema

		if len(ap.Type) == 0 {
			ap.Type = spec.StringOrArray([]string{mapType, objectType})
		} else {
			ap.Type = spec.StringOrArray([]string{mapType, ap.Type[0]}) // massage type so that it is a map of 'type'
		}

		c.processProperty(ap, name, r, method, id, required, jsonRep, myFQNS, chopped, isRequestResource)
	}
}

func (c *APISpecification) processProperty(s *spec.Schema, name string, r *Resource, method *Method, id string, required map[string]bool, jsonRep map[string]any, myFQNS []string,
	chopped, isRequestResource bool,
) {
	newFQNS := prepareNamespace(myFQNS, id, name, chopped)

	var (
		jsonResource map[string]any
		resource     *Resource
	)

	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("A call resourceFromSchema for property %s", name))

	resource, jsonResource, _ = c.resourceFromSchema(s, method, newFQNS, isRequestResource)

	skip := isRequestResource && resource.ReadOnly
	if !skip && resource.ExcludeFromOperations != nil {
		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Exclude [%s] in operation [%s] if in list: %s", name, method.OperationName, resource.ExcludeFromOperations))

		if slices.Contains(resource.ExcludeFromOperations, method.OperationName) {
			log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("[%s] is excluded", name))

			skip = true
		}
	}

	if skip {
		return
	}

	r.Properties[name] = resource
	jsonRep[name] = jsonResource

	if _, ok := required[name]; ok {
		r.Properties[name].Required = true
	}

	log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("resource property %s type: %s", name, r.Properties[name].Type[0]))

	jsonRep[name] = propertyJSON(s, resource, jsonResource)
}

func propertyJSON(s *spec.Schema, resource *Resource, object map[string]any) any {
	switch {
	case strings.EqualFold(resource.Type[0], objectType):
		return object
	case strings.EqualFold(resource.Type[0], arrayType):
		if s.Items == nil || s.Items.Schema == nil {
			return []map[string]any{object}
		}

		resource.Description = string(formatter.Markdown([]byte(s.Description)))

		if len(object) > 0 {
			return []map[string]any{object}
		}

		var primitives []string
		// Fully filtered read-only objects retain their original nil example.
		if len(resource.Type) > 1 {
			primitives = []string{resource.Type[1]}
		}

		return primitives
	case strings.EqualFold(resource.Type[0], mapType):
		if strings.EqualFold(resource.Type[1], objectType) {
			return object
		}

		return resource.Type[1]
	default:
		return resource.Type[0]
	}
}

func (p *Parameter) setType(src spec.Parameter) {
	if src.Type == arrayType {
		if src.CollectionFormat == "" {
			src.CollectionFormat = collectionCSV
		}

		p.Type = append(p.Type, src.Type)
		p.CollectionFormat = src.CollectionFormat
		p.CollectionFormatDescription = collectionFormatDescription(src.CollectionFormat)
	}

	var (
		ptype  string
		format string
	)

	if src.Type == arrayType {
		ptype = src.Items.Type
		format = src.Items.Format
	} else {
		ptype = src.Type
		format = src.Format
	}

	if format != "" {
		ptype = format
	}

	p.Type = append(p.Type, ptype)
}

func (p *Parameter) setEnums(src spec.Parameter) {
	var ea []any

	if src.Type == arrayType {
		ea = src.Items.Enum
	} else {
		ea = src.Enum
	}

	es := make([]string, 0, len(ea))
	for _, e := range ea {
		es = append(es, fmt.Sprintf("%s", e))
	}

	p.Enum = es
}

func (r *Response) compileHeaders(sr *spec.Response) {
	if sr.Headers == nil {
		return
	}

	for name := range sr.Headers {
		params := sr.Headers[name]
		header := &Header{
			Description: string(formatter.Markdown([]byte(params.Description))),
			Name:        name,
		}

		htype := getType(params)

		if params.Type == arrayType {
			if params.CollectionFormat == "" {
				params.CollectionFormat = collectionCSV
			}

			header.Type = append(header.Type, params.Type)
			header.CollectionFormat = params.CollectionFormat
			header.CollectionFormatDescription = collectionFormatDescription(params.CollectionFormat)
		}

		format := getFormat(params)
		if format != "" {
			htype = format
		}

		header.Type = append(header.Type, htype)
		header.Enum = getEnums(params)

		r.Headers = append(r.Headers, *header)
	}
}

func (api *APIGroup) getMethodSortKey(path, method, operation, navigation, summary string) string {
	// Handle a list of sort-by values, so that ordering can be fixed.
	// Sorting by path alone does not work because ordering changes around GET/POST/PUT Etc
	var key strings.Builder

	for _, sortby := range api.MethodSortBy {
		switch sortby {
		case parameterPath:
			key.WriteString(path)
		case sortMethod:
			key.WriteString(method)
		case sortOperation:
			key.WriteString(operation)
		case sortNavigation:
			key.WriteString(navigation)
		case sortSummary:
			key.WriteString(summary)
		}

		key.WriteString("~")
	}

	if key.Len() == 0 {
		return summary
	}

	return key.String()
}

func getTags(specification *spec.Swagger) []spec.Tag {
	tags := make([]spec.Tag, 0)
	tags = append(tags, specification.Tags...)

	if len(tags) == 0 {
		tags = append(tags, spec.Tag{})
	}

	return tags
}

func jsonResourceToString(jsonres map[string]any, isArray bool) string {
	// If the resource is an array, then append json object to outer array, else serialize the object.
	var example []byte

	if isArray {
		arrayObj := []map[string]any{jsonres}
		example, _ = jsonMarshalIndent(arrayObj)
	} else {
		example, _ = jsonMarshalIndent(jsonres)
	}

	return string(example)
}

func checkPropertyType(s *spec.Schema) string {
	/*
	   (string) (len=12) "string_array": (spec.Schema) {
	    SchemaProps: (spec.SchemaProps) {
	     Description: (string) (len=16) "Array of strings",
	     Type: (spec.StringOrArray) (len=1 cap=1) { (string) (len=5) "array" },
	     Items: (*spec.SchemaOrArray)(0xc8205bb000)({
	      Schema: (*spec.Schema)(0xc820202480)({
	       SchemaProps: (spec.SchemaProps) {
	        Type: (spec.StringOrArray) (len=1 cap=1) { (string) (len=6) "string" },
	       },
	      }),
	     }),
	    },
	   }
	*/
	ptype := "primitive"

	if s.Type == nil {
		ptype = objectType
	}

	sOrig := s.Type

	if s.Items != nil {
		if s.Type.Contains(arrayType) {
			if s.Items.Schema != nil {
				s = s.Items.Schema
			} else {
				s = &s.Items.Schemas[0] // - Main schema [1] = Additional properties? See online swagger editior.
			}

			if s.Type == nil {
				ptype = "array of objects"
				if s.Type != nil {
					ptype = "array of SOMETHING"
				}
			} else if s.Type.Contains(arrayType) {
				ptype = "array of primitives"
			} else {
				ptype = fmt.Sprintf("%s", sOrig)
			}
		} else {
			ptype = "Some object"
		}
	}

	return ptype
}

func prepareNamespace(myFQNS []string, id, name string, chopped bool) []string {
	newFQNS := append([]string{}, myFQNS...) // create slice

	if chopped && id != "" {
		log().Log(context.Background(), logger.LevelTrace, fmt.Sprintf("Append ID onto newFQNZ %s + %q", newFQNS, id))
		newFQNS = append(newFQNS, id)
	}

	newFQNS = append(newFQNS, name)

	return newFQNS
}

// titleToKebab convert a Title string to kebab.
func titleToKebab(s string) string {
	return strings.ReplaceAll(
		string(kababExclude.ReplaceAll([]byte(strings.ToLower(s)), []byte(""))),
		" ", "-")
}

// camelToKebab converts camel case to kebab.
func camelToKebab(s string) string {
	return strings.ReplaceAll(snaker.CamelToSnake(s), "_", "-")
}

func (p *Parser) loadSpec(location string) (*loads.Document, error) {
	log().Info(fmt.Sprintf("Importing OpenAPI specifications from %s", location))

	raw, err := loading.LoadFromFileOrHTTP(location)
	if err != nil {
		log().Error(fmt.Sprintf("Error: go-openapi/swag failed to load spec [%s]: %s", location, err))

		return nil, err
	}

	document, err := loads.Analyzed(json.RawMessage(p.replacer.Replace(string(raw))), "")
	if err != nil {
		log().Error(fmt.Sprintf("Error: go-openapi/loads failed to analyze spec: %s", err))

		return nil, err
	}

	document, err = document.Expanded()
	if err != nil {
		log().Error(fmt.Sprintf("Error: go-openapi/loads failed to expand spec: %s", err))
	}

	return document, err
}

// jsonMarshalIndent Wrapper around MarshalIndent to prevent < > & from being escaped.
func jsonMarshalIndent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "    ")

	b = bytes.ReplaceAll(b, []byte("\\u003c"), []byte("<"))
	b = bytes.ReplaceAll(b, []byte("\\u003e"), []byte(">"))
	b = bytes.ReplaceAll(b, []byte("\\u0026"), []byte("&"))

	return b, err
}

func isLocalSpecURL(specURL string) bool {
	match, err := regexp.MatchString("(?i)^https?://.+", specURL)
	if err != nil {
		panic(fmt.Sprintf("Attempted to match against an invalid regexp: %s", err))
	}

	return !match
}

func (p *Parser) normalizeSpecLocation(specLocation string) string {
	if isLocalSpecURL(specLocation) {
		log().Debug(fmt.Sprintf("SpecDir = %s", p.settings.SpecDir))

		base, err := filepath.Abs(p.settings.SpecDir)
		if err != nil {
			log().Error(fmt.Sprintf("Error forming specification path: %s", err))
		}

		base = filepath.ToSlash(base)
		log().Debug(fmt.Sprintf("SpecDir (base) = %s", base))

		return filepath.Join(base, specLocation)
	}

	return specLocation
}

// OpenAPI/Swagger/go-openAPI define a Header object and an Items object. A
// Header _can_ be an Items object, if it is an array. Annoyingly, a Header
// object is the same as Items but with an additional Description member.
// It would have been nice to treat Header.Items as though it were Header in
// the case of an array...
// Solve both problems by defining accessor methods that will do the "right thing"
// in the case of an array.
func getType(h spec.Header) string {
	if h.Type == arrayType {
		return h.Items.Type
	}

	return h.Type
}

func getFormat(h spec.Header) string {
	if h.Type == arrayType {
		return h.Items.Format
	}

	return h.Format
}

func getEnums(h spec.Header) []string {
	var ea []any

	if h.Type == arrayType {
		ea = h.Items.Enum
	} else {
		ea = h.Enum
	}

	es := make([]string, 0, len(ea))
	for _, e := range ea {
		es = append(es, fmt.Sprintf("%s", e))
	}

	return es
}

func isPrivate(exts spec.Extensions) bool {
	if pv, ok := exts.GetString(visibilityExt); ok {
		return pv == "private"
	}

	return false
}

func collectionFormatDescription(format string) string {
	return collectionTable[format]
}
