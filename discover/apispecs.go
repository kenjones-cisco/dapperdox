package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-openapi/loads"
	"github.com/go-openapi/spec"
	"github.com/go-openapi/swag"
	wraperrors "github.com/pkg/errors"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover/models"
)

const (
	extKeyVisibility = "x-visibility"
	extKeyGroupBy    = "x-groupby"

	groupByDefault = "Common APIs"
)

func (d *Discoverer) fetchAPISpecs() map[string][]byte {
	specs, _ := fetchAPIResults(d.fetchContext(), d.data.services.List())
	if len(specs) == 0 {
		return nil
	}

	return specs
}

func fetchAPIResults(ctx context.Context, services []*models.Service) (map[string][]byte, map[string]string) {
	newSpecs := make(map[string][]byte)
	failures := make(map[string]string)
	active := make(map[string]struct{})

	for _, service := range services {
		for key := range serviceAPIKeys(service) {
			active[key] = struct{}{}
		}
	}

	rwd, err := loadRewritesDoc()
	if err != nil {
		log().Error("unable to load rewrites doc")

		for key := range active {
			failures[key] = "configuration_failed"
		}

		return newSpecs, failures
	}

	for _, service := range services {
		if service.Hostname == "" || isIgnoredSvc(service.Hostname) {
			log().Warnf("invalid service %q", service.Hostname)

			continue
		}

		for _, port := range service.Ports {
			if ctx.Err() != nil {
				log().Debug("specification refresh canceled")

				return newSpecs, failures
			}

			if !port.Protocol.IsHTTP() {
				continue
			}

			hostName := service.Hostname
			portNum := port.Port

			path, data, err := handleSpec(ctx, rwd, hostName, portNum)
			if err != nil {
				if ctx.Err() != nil {
					log().Debug("specification refresh canceled")

					return newSpecs, failures
				}

				log().Errorf("unable to load and process spec for API %q", hostName)

				if _, activeAPI := active[hostName]; activeAPI {
					failures[hostName] = "spec_fetch_or_validation_failed"
				}

				continue
			}

			newSpecs[path] = data

			delete(failures, path)
		}
	}

	for key := range active {
		if _, succeeded := newSpecs[key]; succeeded {
			delete(failures, key)

			continue
		}

		if _, failed := failures[key]; !failed {
			failures[key] = "spec_fetch_failed"
		}
	}

	return newSpecs, failures
}

func loadRewritesDoc() (*loads.Document, error) {
	// if there are rewrite configuration defined,
	// ensure we are able to load them
	var rewritesDoc *loads.Document

	if viper.GetString(config.SpecRewrites) != "" {
		log().Info(viper.GetString(config.SpecRewrites))

		data, err := swag.YAMLDoc(viper.GetString(config.SpecRewrites))
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

func handleSpec(ctx context.Context, rwd *loads.Document, hostName string, portNum int) (string, []byte, error) {
	svcSpec, err := loadSpec(ctx, fmt.Sprintf("%s:%d", hostName, portNum))
	if err != nil {
		return "", nil, err
	}

	return processSpec(hostName, rwd.Spec(), svcSpec)
}

func loadSpec(ctx context.Context, location string) (*spec.Swagger, error) {
	if location == "" {
		return nil, wraperrors.New("api location has no value")
	}

	u := &url.URL{Host: location, Scheme: "http", Path: "swagger.json"}

	log().Debugf("apiLoader location: %s", u.String())

	data, err := loadSpecBytes(ctx, u.String())
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

func loadSpecBytes(ctx context.Context, location string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, http.NoBody)
	if err != nil {
		return nil, err
	}

	if swag.LoadHTTPBasicAuthUsername != "" && swag.LoadHTTPBasicAuthPassword != "" {
		req.SetBasicAuth(swag.LoadHTTPBasicAuthUsername, swag.LoadHTTPBasicAuthPassword)
	}

	for key, value := range swag.LoadHTTPCustomHeaders {
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: viper.GetDuration(config.DiscoverySpecLoadTimeout)}

	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not access document at %q [%s]: %w", location, response.Status, swag.ErrLoader)
	}

	return io.ReadAll(response.Body)
}

func processSpec(hostname string, rewritesSpec, svcSpec *spec.Swagger) (string, []byte, error) {
	if svcSpec == nil {
		return "", nil, wraperrors.New("service spec should not be nil")
	}

	removePrivateAPIs(svcSpec)

	removePrivateDefinitions(svcSpec)

	applyGrouping(svcSpec)

	if rewritesSpec != nil {
		applyRewrites(rewritesSpec, svcSpec)
	}

	// using MarshalIndent to maintain formatting for Dapperdox spec download
	outdata, err := json.MarshalIndent(svcSpec, "", "  ")
	if err != nil {
		return "", nil, wraperrors.Wrap(err, "unable to marshal final spec")
	}

	return hostname, outdata, nil
}

func removePrivateAPIs(svcSpec *spec.Swagger) {
	pathsRef := svcSpec.Paths

	if pathsRef == nil {
		log().Warning("no API paths defined")

		return
	}

	// iterate over Paths map
	for k, v := range pathsRef.Paths {
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

	for k, v := range definitionsRef {
		if isPrivate(v.Extensions) {
			delete(definitionsRef, k)
		}
	}
}

func applyGrouping(svcSpec *spec.Swagger) {
	// create extensions if non exist
	if svcSpec.Extensions == nil {
		svcSpec.Extensions = make(map[string]interface{})
	}

	// add a new grouping extension based on the provided grouping converters,
	// matching the provided grouping key located at the spec's root-level extensions
	var isGrouped bool

	if gkey := viper.GetString(config.DiscoveryGroupingKey); gkey != "" {
		if gval, ok := svcSpec.Extensions[gkey]; ok {
			if newgval, ok := viper.GetStringMapString(config.DiscoveryGroupingConverters)[gval.(string)]; ok {
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
		if customgroup, ok := viper.GetStringMapString(config.SpecGroupings)[tag.Name]; ok {
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

	for k, v := range rewrites.Extensions {
		// note: not leveraging spec.Extensions.Add() since it applies ToLower() to key value,
		//  affecting the case-sensitive key lookup by dapperdox
		//  - https://github.com/DapperDox/dapperdox/blob/e343254818a8c67c29de6b192e11b0fcb0703800/spec/spec.go#L345,L350
		svcSpec.Extensions[k] = v
	}
}

func isPrivate(exts spec.Extensions) bool {
	if pv, ok := exts.GetString(extKeyVisibility); ok {
		return pv == "private"
	}

	return false
}

func isIgnoredSvc(name string) bool {
	for _, ignore := range viper.GetStringSlice(config.DiscoveryServiceIgnoreList) {
		if strings.HasPrefix(name, ignore) {
			return true
		}
	}

	return false
}
