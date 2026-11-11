package spec

import (
	"encoding/json"
	"testing"

	openapi "github.com/go-openapi/spec"
)

func TestMethodSortKeyPreservesFieldSeparators(t *testing.T) {
	api := APIGroup{MethodSortBy: []string{"path", "method", "operation", "navigation", "summary"}}
	if got := api.getMethodSortKey("/pets", "get", "listPets", "List", "All pets"); got != "/pets~get~listPets~List~All pets~" {
		t.Fatalf("composite sort key = %q", got)
	}

	api.MethodSortBy = nil
	if got := api.getMethodSortKey("/pets", "get", "", "", "All pets"); got != "All pets" {
		t.Fatalf("default sort key = %q", got)
	}
}

func TestPropertyJSONPreservesResourceExamples(t *testing.T) {
	for _, test := range []struct {
		name   string
		types  []string
		items  *openapi.SchemaOrArray
		object map[string]any
		want   string
	}{
		{"object", []string{"object"}, nil, map[string]any{"name": "string"}, `{"name":"string"}`},
		{"primitive", []string{"integer"}, nil, nil, `"integer"`},
		{"object map", []string{"map", "object"}, nil, map[string]any{"name": "string"}, `{"name":"string"}`},
		{"primitive map", []string{"map", "integer"}, nil, nil, `"integer"`},
		{"primitive array", []string{"array", "string"}, &openapi.SchemaOrArray{Schema: &openapi.Schema{}}, nil, `["string"]`},
		{"filtered array", []string{"array"}, &openapi.SchemaOrArray{Schema: &openapi.Schema{}}, nil, `null`},
		{"object array", []string{"array"}, &openapi.SchemaOrArray{Schema: &openapi.Schema{}}, map[string]any{"name": "string"}, `[{"name":"string"}]`},
		{"array without items", []string{"array"}, nil, nil, `[null]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := &openapi.Schema{SchemaProps: openapi.SchemaProps{Items: test.items}}
			resource := &Resource{Type: test.types}

			data, err := json.Marshal(propertyJSON(schema, resource, test.object))
			if err != nil {
				t.Fatal(err)
			}

			if string(data) != test.want {
				t.Fatalf("example = %s, want %s", data, test.want)
			}
		})
	}
}
