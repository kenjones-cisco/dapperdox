package timeout

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerPreservesCommittedResponseOnTimeout(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "not committed"
		if committed {
			name = "committed"
		}

		t.Run(name, func(t *testing.T) {
			finished := make(chan error, 1)
			handler := Handler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if committed {
					w.WriteHeader(http.StatusAccepted)
					_, _ = io.WriteString(w, "partial")
				}

				<-req.Context().Done()

				w.WriteHeader(http.StatusCreated)

				_, err := io.WriteString(w, "late")
				finished <- err
			}), 25*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusRequestTimeout)
				_, _ = io.WriteString(w, "timeout")
			}))

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

			select {
			case err := <-finished:
				if !errors.Is(err, http.ErrHandlerTimeout) {
					t.Fatalf("late write error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("timeout did not cancel handler context")
			}

			wantStatus, wantBody := http.StatusRequestTimeout, "timeout"
			if committed {
				wantStatus, wantBody = http.StatusAccepted, "partial"
			}

			if response.Code != wantStatus || response.Body.String() != wantBody {
				t.Fatalf("response = %d %q, want %d %q", response.Code, response.Body.String(), wantStatus, wantBody)
			}
		})
	}
}

func TestHandlerCompletesBeforeTimeout(t *testing.T) {
	handler := Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Result", "finished")
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "complete")
	}), time.Hour, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("successful request called failure handler")
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

	if response.Code != http.StatusCreated || response.Body.String() != "complete" || response.Header().Get("X-Result") != "finished" {
		t.Fatalf("completed response changed: %d %q %v", response.Code, response.Body.String(), response.Header())
	}
}

func TestTimeoutCancelsBackgroundRequest(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	expired := make(chan time.Time, 1)
	handler := &handler{
		handler: http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
			close(started)
			<-req.Context().Done()
			close(finished)
		}),
		timeout: func() <-chan time.Time { return expired },
		failHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusRequestTimeout)
		}),
	}
	done := make(chan struct{})

	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
		close(done)
	}()

	<-started

	expired <- time.Now()

	<-done

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("timed-out background request retained its generation")
	}
}
