package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover"
	log "github.com/kenjones-cisco/dapperdox/logger"
)

type snapshotDiscovery struct{ value discover.Snapshot }

func (d *snapshotDiscovery) Snapshot() discover.Snapshot { return d.value }
func (d *snapshotDiscovery) Specs() map[string][]byte    { return d.value.Specs }
func (d *snapshotDiscovery) RegisterOnChangeFunc(func()) {}
func (d *snapshotDiscovery) Run()                        {}
func (d *snapshotDiscovery) Shutdown()                   {}

type deletingDiscovery struct {
	snapshotDiscovery

	reads int
}

func (d *deletingDiscovery) Snapshot() discover.Snapshot {
	d.reads++
	if d.reads == 2 {
		delete(d.value.APIs, "common")
		delete(d.value.Specs, "common")

		d.value.Revision++
	}

	return d.value
}

func testUpdater(t *testing.T) (*Updater, *snapshotDiscovery) {
	t.Helper()
	config.Restore()

	if err := log.SetLevel("warn"); err != nil {
		t.Fatal(err)
	}

	viper.Set(config.DefaultAssetsDir, "../assets")
	viper.Set(config.DiscoveryInitialDelay, "1h")

	d := &snapshotDiscovery{value: discover.Snapshot{
		Synced: true, Specs: make(map[string][]byte), APIs: make(map[string]discover.APIStatus),
	}}

	u, err := NewAutoDiscoverUpdater(d)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { u.Close(); config.Restore() })

	return u, d
}

func fixture(t *testing.T) []byte {
	t.Helper()

	raw, err := os.ReadFile("../fixtures/common_api.json")
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func refresh(u *Updater, d *snapshotDiscovery, key string, raw []byte) {
	d.value.Specs[key] = raw
	d.value.APIs[key] = discover.APIStatus{LastSuccess: time.Now().UTC()}
	d.value.Revision++

	u.onChange()
	u.update()
}

func request(h http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, http.NoBody))

	return response
}

func changedFixture(raw []byte, revision int) []byte {
	return bytes.Replace(raw, []byte("Manages provisioning of AWS resources"),
		[]byte("Description revision "+strconv.Itoa(revision)), 1)
}

func TestUpdaterBoundedGenerations(t *testing.T) {
	u, d := testUpdater(t)
	raw := fixture(t)
	refresh(u, d, "common", raw)
	unchanged := u.current.Load().generation
	initialNumber := u.current.Load().status.Generation

	for range 200 {
		refresh(u, d, "common", raw)
	}

	if got := u.current.Load(); got.generation != unchanged || got.status.Generation != initialNumber {
		t.Fatal("unchanged specs rebuilt documentation")
	}
	// Warm parser/template caches before checking retained memory on genuine changes.
	for i := range 10 {
		refresh(u, d, "common", changedFixture(raw, i))
	}

	runtime.GC()

	var before, after runtime.MemStats

	runtime.ReadMemStats(&before)

	routes := u.current.Load().status.Routes

	for i := range 200 {
		refresh(u, d, "common", changedFixture(raw, i+20))

		if u.current.Load().status.Routes != routes {
			t.Fatal("route count changed at equivalent catalog size")
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("changed refreshes=200 routes=%d retained heap delta=%d", routes, int64(after.HeapAlloc)-int64(before.HeapAlloc))

	if after.HeapAlloc > before.HeapAlloc+1024*1024 {
		t.Fatal("equivalent catalog retained more than 1 MiB after GC")
	}

	if !strings.Contains(u.last["common"].parsed.APIInfo.Description, "Description revision 219") {
		t.Fatal("changed documentation did not publish the newest content")
	}

	runtime.KeepAlive(u)
}

func TestUpdaterFailureDoesNotBlockOtherChanges(t *testing.T) {
	u, d := testUpdater(t)
	raw := fixture(t)
	other := bytes.Replace(raw, []byte("AWS Service"), []byte("Other Service"), 1)
	refresh(u, d, "common", raw)
	refresh(u, d, "other", other)
	d.value.APIs["common"] = discover.APIStatus{Failure: "fetch_failed"}
	refresh(u, d, "other", changedFixture(other, 50))

	view := u.current.Load()
	if view.status.APIs["common"].State != "stale" ||
		!strings.Contains(u.last["other"].parsed.APIInfo.Description, "revision 50") {
		t.Fatal("one failed API prevented an independent valid change")
	}

	refresh(u, d, "common", []byte("{"))

	if u.current.Load().status.APIs["common"].State != "stale" ||
		!bytes.Equal(u.last["common"].raw, raw) {
		t.Fatal("invalid specification replaced last-good documentation")
	}
}

func TestUpdaterLastGoodDeletionAndRecovery(t *testing.T) {
	u, d := testUpdater(t)
	raw := fixture(t)
	refresh(u, d, "common", raw)
	initial := u.current.Load()
	id := u.last["common"].parsed.ID
	d.value.APIs["common"] = discover.APIStatus{Failure: "fetch_failed"}

	u.onChange()
	u.update()

	if got := u.current.Load(); got.status.APIs["common"].State != "stale" || got.generation != initial.generation {
		t.Fatal("fetch failure lost last-good documentation or rebuilt routes")
	}

	page := request(u, "/"+id+"/reference")
	if page.Code != http.StatusOK || !bytes.Contains(bytes.ToLower(page.Body.Bytes()), []byte("stale")) {
		t.Fatalf("stale warning missing: status=%d", page.Code)
	}

	generation := u.current.Load().status.Generation
	refresh(u, d, "common", raw)

	if got := u.current.Load(); got.status.APIs["common"].State != "available" || got.status.Generation != generation {
		t.Fatal("unchanged recovery should clear stale state without rebuilding")
	}

	delete(d.value.APIs, "common")
	delete(d.value.Specs, "common")
	u.onChange()
	u.update()

	if got := u.current.Load(); !got.status.Ready || len(got.status.APIs) != 0 || len(u.last) != 0 {
		t.Fatal("last deletion did not publish ready empty catalog")
	}

	if code := request(u, "/common/api.json").Code; code != http.StatusNotFound {
		t.Fatalf("deleted raw spec returned %d", code)
	}
}

func TestUpdaterPartialAndRejectedBuild(t *testing.T) {
	u, d := testUpdater(t)
	d.value.Synced = false

	u.update()

	if code := request(u, "/readyz").Code; code != http.StatusServiceUnavailable {
		t.Fatal("unsynchronized catalog became ready")
	}

	d.value.Synced = true
	raw := fixture(t)
	refresh(u, d, "common", raw)
	refresh(u, d, "bad", []byte("{"))

	if got := u.current.Load(); !got.status.Ready || got.status.APIs["bad"].State != "unavailable" {
		t.Fatal("invalid new API should not prevent partial readiness")
	}

	old := u.current.Load()
	refresh(u, d, "duplicate-title", raw)

	if got := u.current.Load(); got.generation != old.generation || got.status.Failure != "generation_build_failed" {
		t.Fatal("duplicate identifiers published a broken generation")
	}

	if _, exists := u.last["duplicate-title"]; exists {
		t.Fatal("rejected candidate committed last-good state")
	}
}

func TestUpdaterConcurrentRequestsAndOldGeneration(t *testing.T) {
	u, d := testUpdater(t)
	raw := fixture(t)
	refresh(u, d, "common", raw)
	old := u.current.Load()
	started, release := make(chan struct{}), make(chan struct{})

	old.generation.router.HandleFunc("/blocked", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release

		_, _ = w.Write([]byte("old"))
	})

	done := make(chan *httptest.ResponseRecorder, 1)

	go func() { done <- request(u, "/blocked") }()

	<-started
	refresh(u, d, "common", append(bytes.Clone(raw), ' '))
	close(release)

	if response := <-done; response.Body.String() != "old" {
		t.Fatal("in-flight request did not complete on old generation")
	}

	if request(u, "/blocked").Code != http.StatusNotFound {
		t.Fatal("new requests retained old-only route")
	}

	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for range 20 {
				response := request(u, "/common/api.json")
				if response.Code != http.StatusOK || !json.Valid(response.Body.Bytes()) {
					t.Error("inconsistent raw spec response")
				}
			}
		})
	}

	for i := range 20 {
		refresh(u, d, "common", append(bytes.Clone(raw), bytes.Repeat([]byte(" "), i+2)...))
	}

	readers.Wait()
}

func TestUpdaterManagementIsolationAndClose(t *testing.T) {
	u, d := testUpdater(t)
	refresh(u, d, "common", fixture(t))

	if request(u, "/status").Code != http.StatusNotFound {
		t.Fatal("operator status exposed on public router")
	}

	if response := request(u.ManagementHandler(), "/status"); response.Code != http.StatusOK || !json.Valid(response.Body.Bytes()) {
		t.Fatal("operator status unavailable")
	}

	var closes sync.WaitGroup
	for range 4 {
		closes.Go(func() { ; u.Close() })
	}

	closes.Wait()
}

func TestUpdaterSupersededCandidate(t *testing.T) {
	u, source := testUpdater(t)
	source.value.Specs["common"] = fixture(t)
	source.value.APIs["common"] = discover.APIStatus{LastSuccess: time.Now().UTC()}
	source.value.Revision = 1
	deleting := &deletingDiscovery{snapshotDiscovery: *source}
	u.d = deleting
	u.update()

	if u.current.Load().generation != nil {
		t.Fatal("candidate published after its API was deleted")
	}

	u.update()

	if view := u.current.Load(); !view.status.Ready || len(view.status.APIs) != 0 {
		t.Fatal("pending deletion was lost while rejecting obsolete build")
	}
}

func TestUpdaterCoalescedDeleteRecreate(t *testing.T) {
	u, d := testUpdater(t)
	d.value.Specs["common"] = fixture(t)
	d.value.APIs["common"] = discover.APIStatus{Generation: 1, LastSuccess: time.Now().UTC()}

	u.update()

	d.value.APIs["common"] = discover.APIStatus{Generation: 2, Failure: "fetch_failed"}

	u.onChange()
	u.update()

	if got := u.current.Load().status.APIs["common"]; got.State != "unavailable" || !got.LastSuccess.IsZero() {
		t.Fatal("replacement service inherited a deleted instance's last-good state")
	}

	if len(u.last) != 0 || request(u, "/common/api.json").Code != http.StatusNotFound {
		t.Fatal("deleted instance's documentation survived coalesced recreation")
	}
}

func TestConsumingDocsRoutes(t *testing.T) {
	assets := os.Getenv("DAPPERDOX_TEST_ASSETS")
	if assets == "" {
		t.Skip("set DAPPERDOX_TEST_ASSETS to the consuming docs assets directory")
	}

	config.Restore()
	viper.Set(config.DefaultAssetsDir, "../assets")
	viper.Set(config.AssetsDir, assets)
	viper.Set(config.Theme, "cisco")
	viper.Set(config.DiscoveryInitialDelay, "1h")

	d := &snapshotDiscovery{value: discover.Snapshot{
		Synced: true, Specs: make(map[string][]byte), APIs: make(map[string]discover.APIStatus),
	}}

	u, err := NewAutoDiscoverUpdater(d)
	if err != nil {
		t.Fatal(err)
	}

	defer u.Close()
	defer config.Restore()

	refresh(u, d, "common", fixture(t))

	if !u.current.Load().status.Ready {
		t.Fatal("consuming theme failed generation construction")
	}

	for _, path := range []string{"/guides/overview", "/common/api.json", "/" + u.last["common"].parsed.ID + "/reference"} {
		response := request(u, path)
		if response.Code != http.StatusOK {
			t.Errorf("%s returned %d", path, response.Code)
		}
	}
}
