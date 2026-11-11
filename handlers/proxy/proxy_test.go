package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestProxyRewritePreservesTargetAndRequest(t *testing.T) {
	requests := make(chan *http.Request, 1)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())

		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()

	router := mux.NewRouter()
	register(router, "/api", upstream.URL+"/base?fixed=1")

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://docs.example/api/pets?fields=name", http.NoBody)
	request.Header.Set("X-Forwarded-For", "untrusted")

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("proxy status = %d", response.Code)
	}

	received := <-requests
	if received.URL.Path != "/base/api/pets" || received.URL.RawQuery != "fixed=1&fields=name" {
		t.Fatalf("target path/query changed: %s", received.URL)
	}

	if received.Host != strings.TrimPrefix(upstream.URL, "http://") {
		t.Fatalf("upstream Host = %q", received.Host)
	}

	if got := received.Header.Get("X-Forwarded-For"); got != "192.0.2.1" {
		t.Fatalf("forwarded client address = %q", got)
	}
}
