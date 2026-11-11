package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-openapi/loads"
	"github.com/go-openapi/spec"
	"github.com/go-openapi/swag/loading"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover/models"
)

const (
	schemeHTTP        = "http"
	visibilityPrivate = "private"
	failureSpecFetch  = "spec_fetch_failed"
	extKeyVisibility  = "x-visibility"
	extKeyGroupBy     = "x-groupby"

	groupByDefault = "Common APIs"
)

var (
	errMissingAPILocation = errors.New("api location has no value")
	errMissingServiceSpec = errors.New("service spec should not be nil")
)

type specLoader struct{ settings config.Settings }

func (d *Discoverer) fetchAPISpecs() map[string][]byte {
	specs, _ := d.loader().fetchAPIResults(d.fetchContext(), d.data.services.List())
	if len(specs) == 0 {
		return nil
	}

	return specs
}

func (l specLoader) fetchAPIResults(ctx context.Context, services []*models.Service) (newSpecs map[string][]byte, failures map[string]string) {
	newSpecs = make(map[string][]byte)
	failures = make(map[string]string)
	active := make(map[string]struct{})

	for _, service := range services {
		for key := range serviceAPIKeysWithConfig(service, l.settings) {
			active[key] = struct{}{}
		}
	}

	rwd, err := l.loadRewritesDoc()
	if err != nil {
		log().ErrorContext(ctx, "unable to load rewrites doc")

		for key := range active {
			failures[key] = "configuration_failed"
		}

		return newSpecs, failures
	}

	for _, service := range services {
		if service.Hostname == "" || l.isIgnoredSvc(service.Hostname) {
			log().WarnContext(ctx, fmt.Sprintf("invalid service %q", service.Hostname))

			continue
		}

		l.fetchServiceResults(ctx, rwd, service, newSpecs, failures)

		if ctx.Err() != nil {
			return newSpecs, failures
		}
	}

	for key := range active {
		if _, succeeded := newSpecs[key]; succeeded {
			delete(failures, key)

			continue
		}

		if _, failed := failures[key]; !failed {
			failures[key] = failureSpecFetch
		}
	}

	return newSpecs, failures
}

func (l specLoader) fetchServiceResults(ctx context.Context, rewrites *loads.Document, service *models.Service, specs map[string][]byte, failures map[string]string) {
	for _, port := range service.Ports {
		if ctx.Err() != nil {
			log().DebugContext(ctx, "specification refresh canceled")

			return
		}

		if !port.Protocol.IsHTTP() {
			continue
		}

		path, data, err := l.handleSpec(ctx, rewrites, service.Hostname, port.Port)
		if err != nil {
			if ctx.Err() != nil {
				log().DebugContext(ctx, "specification refresh canceled")

				return
			}

			log().ErrorContext(ctx, "unable to load and process API specification", slog.String("api", service.Hostname))
			failures[service.Hostname] = "spec_fetch_or_validation_failed"

			continue
		}

		specs[path] = data
		delete(failures, path)
	}
}

func (l specLoader) loadRewritesDoc() (*loads.Document, error) {
	// if there are rewrite configuration defined,
	// ensure we are able to load them
	var rewritesDoc *loads.Document

	if l.settings.SpecRewrites != "" {
		log().Info(l.settings.SpecRewrites)

		data, err := loading.YAMLDoc(l.settings.SpecRewrites)
		if err != nil {
			return rewritesDoc, err
		}

		rewritesDoc, err = loads.Analyzed(data, "")
		if err != nil {
			return rewritesDoc, err
		}
	}

	return rewritesDoc, nil
}

func (l specLoader) handleSpec(ctx context.Context, rwd *loads.Document, hostName string, portNum int) (path string, data []byte, err error) {
	svcSpec, err := l.loadSpec(ctx, fmt.Sprintf("%s:%d", hostName, portNum))
	if err != nil {
		return "", nil, err
	}

	return l.processSpec(hostName, rwd.Spec(), svcSpec)
}

func loadSpec(ctx context.Context, location string) (*spec.Swagger, error) {
	return (specLoader{config.Snapshot()}).loadSpec(ctx, location)
}

func (l specLoader) loadSpec(ctx context.Context, location string) (*spec.Swagger, error) {
	if location == "" {
		return nil, errMissingAPILocation
	}

	u := &url.URL{Host: location, Scheme: schemeHTTP, Path: "swagger.json"}

	log().DebugContext(ctx, fmt.Sprintf("apiLoader location: %s", u.String()))

	data, err := l.loadSpecBytes(ctx, u.String())
	if err != nil {
		return nil, err
	}

	var doc *loads.Document

	doc, err = loads.Analyzed(json.RawMessage(data), "")
	if err != nil {
		return nil, err
	}

	return doc.Spec(), nil
}

func (l specLoader) loadSpecBytes(ctx context.Context, location string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, http.NoBody)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: l.settings.DiscoverySpecLoadTimeout}

	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not access document at %q [%s]: %w", location, response.Status, loading.ErrLoader)
	}

	return io.ReadAll(response.Body)
}

func processSpec(hostname string, rewritesSpec, svcSpec *spec.Swagger) (path string, data []byte, err error) {
	return (specLoader{config.Snapshot()}).processSpec(hostname, rewritesSpec, svcSpec)
}

func (l specLoader) processSpec(hostname string, rewritesSpec, svcSpec *spec.Swagger) (path string, data []byte, err error) {
	if svcSpec == nil {
		return "", nil, errMissingServiceSpec
	}

	removePrivateAPIs(svcSpec)

	removePrivateDefinitions(svcSpec)

	l.applyGrouping(svcSpec)

	if rewritesSpec != nil {
		applyRewrites(rewritesSpec, svcSpec)
	}

	// using MarshalIndent to maintain formatting for Dapperdox spec download
	outdata, err := json.MarshalIndent(svcSpec, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("unable to marshal final spec: %w", err)
	}

	return hostname, outdata, nil
}

func removePrivateAPIs(svcSpec *spec.Swagger) {
	pathsRef := svcSpec.Paths

	if pathsRef == nil {
		log().Warn("no API paths defined")

		return
	}

	// iterate over Paths map
	for k := range pathsRef.Paths {
		v := pathsRef.Paths[k]
		// remove the entire API path from swagger spec if it's marked as `private`
		if isPrivate(v.Extensions) {
			delete(pathsRef.Paths, k)

			continue
		}

		// remove any defined method from the above path that's marked as `private`
		if v.Get != nil && isPrivate(v.Get.Extensions) {
			v.Get = nil
		}

		if v.Put != nil && isPrivate(v.Put.Extensions) {
			v.Put = nil
		}

		if v.Patch != nil && isPrivate(v.Patch.Extensions) {
			v.Patch = nil
		}

		if v.Post != nil && isPrivate(v.Post.Extensions) {
			v.Post = nil
		}

		if v.Delete != nil && isPrivate(v.Delete.Extensions) {
			v.Delete = nil
		}

		// re-insert modified spec.PathItem value into Paths map
		pathsRef.Paths[k] = v
	}
}

func removePrivateDefinitions(svcSpec *spec.Swagger) {
	definitionsRef := svcSpec.Definitions

	for k := range definitionsRef {
		v := definitionsRef[k]
		if isPrivate(v.Extensions) {
			delete(definitionsRef, k)
		}
	}
}

func (l specLoader) applyGrouping(svcSpec *spec.Swagger) {
	// create extensions if non exist
	if svcSpec.Extensions == nil {
		svcSpec.Extensions = make(map[string]any)
	}

	// add a new grouping extension based on the provided grouping converters,
	// matching the provided grouping key located at the spec's root-level extensions
	var isGrouped bool

	if gkey := l.settings.DiscoveryGroupingKey; gkey != "" {
		if gval, ok := svcSpec.Extensions[gkey]; ok {
			if newgval, ok := l.settings.DiscoveryGroupingConverters[gval.(string)]; ok {
				svcSpec.Extensions.Add(extKeyGroupBy, newgval)

				isGrouped = true
			}
		}
	}

	if !isGrouped {
		svcSpec.Extensions.Add(extKeyGroupBy, groupByDefault)
	}

	// set custom grouping extension if defined
	for _, tag := range svcSpec.Tags {
		if customgroup, ok := l.settings.SpecGroupings[tag.Name]; ok {
			svcSpec.Extensions.Add(extKeyGroupBy, customgroup)
		}
	}
}

func applyRewrites(rewrites, svcSpec *spec.Swagger) {
	// replace spec details for the following values already defined
	if len(rewrites.SecurityDefinitions) > 0 {
		svcSpec.SecurityDefinitions = rewrites.SecurityDefinitions
	}

	if len(rewrites.Security) > 0 {
		svcSpec.Security = rewrites.Security
	}

	if len(rewrites.Schemes) > 0 {
		svcSpec.Schemes = rewrites.Schemes
	}

	// note: not leveraging spec.Extensions.Add() since it applies ToLower() to key value,
	//  affecting the case-sensitive key lookup by dapperdox
	//  - https://github.com/DapperDox/dapperdox/blob/e343254818a8c67c29de6b192e11b0fcb0703800/spec/spec.go#L345,L350
	maps.Copy(svcSpec.Extensions, rewrites.Extensions)
}

func isPrivate(exts spec.Extensions) bool {
	if pv, ok := exts.GetString(extKeyVisibility); ok {
		return pv == visibilityPrivate
	}

	return false
}

func (l specLoader) isIgnoredSvc(name string) bool {
	for _, ignore := range l.settings.DiscoveryServiceIgnoreList {
		if strings.HasPrefix(name, ignore) {
			return true
		}
	}

	return false
}
