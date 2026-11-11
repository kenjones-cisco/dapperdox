package timeout

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", http.NoBody))
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
