package render

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	nethtml "golang.org/x/net/html"
)

// Status describes reader-facing freshness, not internal failure details.
// State "stale" identifies retained last-known-good documentation.
type Status struct {
	State       string
	LastSuccess time.Time
	Name        string
}

type statusKey struct{}

const maxStatusNameRunes = 160

// WithStatus associates a publication's immutable freshness view with a request.
// Keys are public specification IDs; callers must not mutate the map afterward.
func WithStatus(req *http.Request, status map[string]Status) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), statusKey{}, status))
}

func injectStatus(output []byte, binding interface{}) []byte {
	values, ok := binding.(map[string]interface{})
	if !ok {
		if vars, isVars := binding.(Vars); isVars {
			values = vars
		}
	}

	status, _ := values["DocumentationStatus"].(map[string]Status)
	id, hasID := values["ID"].(string)
	keys := make([]string, 0, len(status))

	for key, value := range status {
		if value.State == "stale" && (!hasID || key == id) {
			keys = append(keys, key)
		}
	}

	if len(keys) == 0 {
		return output
	}

	sort.Strings(keys)

	var warning strings.Builder

	for _, key := range keys {
		value := status[key]

		name := value.Name
		if name == "" {
			name = key
		}

		name = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}

			return r
		}, name)
		// Bound warning size independently of untrusted specification titles.
		runes := []rune(name)
		if len(runes) > maxStatusNameRunes {
			name = string(runes[:maxStatusNameRunes])
		}

		last := "unknown"
		if !value.LastSuccess.IsZero() {
			last = value.LastSuccess.UTC().Format(time.RFC3339)
		}

		fmt.Fprintf(&warning, `<div class="documentation-status-warning" data-documentation-state="stale" role="status">Documentation for %s may be out of date. Last successful update: %s.</div>`,
			html.EscapeString(name), html.EscapeString(last))
	}

	index, offset := 0, 0
	tokenizer := nethtml.NewTokenizer(bytes.NewReader(output))

	for {
		token := tokenizer.Next()
		if token == nethtml.ErrorToken {
			break
		}

		offset += len(tokenizer.Raw())

		if token == nethtml.StartTagToken {
			name, _ := tokenizer.TagName()
			if bytes.Equal(name, []byte("body")) {
				index = offset

				break
			}
		}
	}

	result := make([]byte, 0, len(output)+warning.Len())
	result = append(result, output[:index]...)
	result = append(result, warning.String()...)

	return append(result, output[index:]...)
}
