package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/go-openapi/loads"
)

var (
	errMissingTitle = errors.New("specification requires info.title")
	errMissingPaths = errors.New("specification requires paths")
	errMissingID    = errors.New("specification requires a nonempty identifier")
	errDuplicateID  = errors.New("duplicate specification identifier")
)

// Suite is an immutable parsed documentation catalog.
type Suite struct {
	Specifications map[string]*APISpecification
	Groups         map[string][]*APISpecification
}

var parseMu sync.Mutex

// Parse parses one API without changing the catalog used by other requests.
func Parse(location string, raw []byte) (*APISpecification, error) {
	parseMu.Lock()
	defer parseMu.Unlock()

	loadStatusCodes()
	loadReplacer()

	document, err := loads.Analyzed(json.RawMessage(raw), "")
	if err != nil {
		return nil, fmt.Errorf("analyze specification: %w", err)
	}

	document, err = document.Expanded()
	if err != nil {
		return nil, fmt.Errorf("expand specification: %w", err)
	}

	return parseDocument(location, document)
}

type parseError string

func (e parseError) Error() string { return string(e) }

func parseDocument(location string, document *loads.Document) (result *APISpecification, err error) {
	// The legacy recursive parser aborts invalid input using this specific type.
	// Programming errors are deliberately not recovered.
	defer func() {
		if value := recover(); value != nil {
			if failure, ok := value.(parseError); ok {
				result, err = nil, failure
			} else {
				panic(value)
			}
		}
	}()

	if document.Spec().Info == nil || document.Spec().Info.Title == "" {
		return nil, errMissingTitle
	}

	if document.Spec().Paths == nil {
		return nil, errMissingPaths
	}

	result = &APISpecification{}
	result.load(location, document)

	return result, nil
}

// NewSuite assembles validated APIs and rejects ambiguous public identifiers.
func NewSuite(apis map[string]*APISpecification) (*Suite, error) {
	suite := &Suite{
		Specifications: make(map[string]*APISpecification),
		Groups:         make(map[string][]*APISpecification),
	}

	keys := make([]string, 0, len(apis))
	for key := range apis {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		api := apis[key]
		if api == nil || api.ID == "" {
			return nil, errMissingID
		}

		if _, exists := suite.Specifications[api.ID]; exists {
			return nil, fmt.Errorf("%w %q", errDuplicateID, api.ID)
		}

		suite.Specifications[api.ID] = api
		suite.Groups[api.GroupBy] = append(suite.Groups[api.GroupBy], api)
	}

	return suite, nil
}

// LoadSuite loads configured static specifications without publishing globals.
func LoadSuite() (*Suite, error) {
	parseMu.Lock()
	defer parseMu.Unlock()

	loadStatusCodes()
	loadReplacer()

	docs, err := getDocsByDir()
	if err != nil {
		return nil, err
	}

	apis := make(map[string]*APISpecification, len(docs))

	for location, doc := range docs {
		api, err := parseDocument(location, doc)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", location, err)
		}

		apis[location] = api
	}

	return NewSuite(apis)
}
