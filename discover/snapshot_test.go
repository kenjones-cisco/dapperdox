package discover

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover/models"
)

var _ SnapshotProvider = (*Discoverer)(nil)

func TestSnapshotReturnsIndependentCopies(t *testing.T) {
	raw := []byte("specification")
	d := &Discoverer{
		specs: map[string][]byte{"api.example.test": raw},
		apis: map[string]APIStatus{
			"api.example.test": {LastSuccess: time.Now(), Failure: "stale"},
		},
	}

	snapshot := d.Snapshot()
	snapshot.Specs["api.example.test"][0] = 'X'
	delete(snapshot.Specs, "api.example.test")
	snapshot.APIs["api.example.test"] = APIStatus{}
	delete(snapshot.APIs, "api.example.test")

	got := d.Snapshot()
	if string(got.Specs["api.example.test"]) != "specification" {
		t.Fatalf("Snapshot Specs aliases internal bytes: %q", got.Specs["api.example.test"])
	}

	if got.APIs["api.example.test"].Failure != "stale" {
		t.Fatalf("Snapshot APIs aliases internal map: %+v", got.APIs)
	}

	if got.Revision != 0 || got.Synced {
		t.Fatalf("unexpected initial snapshot metadata: %+v", got)
	}
}

func TestSnapshotKeepsLastGoodOnFailureAndRemovesDeletedAPI(t *testing.T) {
	d := newSnapshotTestDiscoverer()
	service := &models.Service{
		Hostname: "api.example.test",
		Ports:    models.PortList{{Protocol: models.ProtocolHTTP, Port: 8080}},
	}

	var (
		mu            sync.Mutex
		spec          = []byte("good-spec")
		failure       string
		fetchRevision uint64
	)

	d.fetch = func(services []*models.Service) (map[string][]byte, map[string]string) {
		mu.Lock()
		defer mu.Unlock()

		fetchRevision = d.Snapshot().Revision

		specs := make(map[string][]byte)
		failures := make(map[string]string)

		for _, current := range services {
			if failure != "" {
				failures[current.Hostname] = failure
			} else {
				specs[current.Hostname] = append([]byte(nil), spec...)
			}
		}

		return specs, failures
	}

	d.updateServices(service, models.EventAdd)

	first := d.Snapshot()
	if !first.Synced || first.Revision == 0 {
		t.Fatalf("initial successful snapshot not published: %+v", first)
	}

	if fetchRevision == 0 || fetchRevision >= first.Revision {
		t.Fatalf("inventory revision was not published before fetch: fetch saw %d, snapshot has %d", fetchRevision, first.Revision)
	}

	if !bytes.Equal(first.Specs[service.Hostname], []byte("good-spec")) {
		t.Fatalf("initial spec = %q", first.Specs[service.Hostname])
	}

	firstSuccess := first.APIs[service.Hostname].LastSuccess
	if firstSuccess.IsZero() || first.APIs[service.Hostname].Failure != "" {
		t.Fatalf("initial API status = %+v", first.APIs[service.Hostname])
	}

	firstGeneration := first.APIs[service.Hostname].Generation
	if firstGeneration == 0 {
		t.Fatal("initial API generation is zero")
	}

	mu.Lock()
	failure = "spec_fetch_failed"
	mu.Unlock()
	d.updateServices(service, models.EventUpdate)

	stale := d.Snapshot()
	if !bytes.Equal(stale.Specs[service.Hostname], []byte("good-spec")) {
		t.Fatalf("failed refresh discarded last-good bytes: %q", stale.Specs[service.Hostname])
	}

	if status := stale.APIs[service.Hostname]; status.Failure != "spec_fetch_failed" || !status.LastSuccess.Equal(firstSuccess) || status.Generation != firstGeneration {
		t.Fatalf("failed refresh status = %+v; want failure and preserved success time", status)
	}

	mu.Lock()
	failure = ""
	spec = []byte("replacement")
	mu.Unlock()
	d.updateServices(service, models.EventUpdate)

	recovered := d.Snapshot()
	if !bytes.Equal(recovered.Specs[service.Hostname], []byte("replacement")) {
		t.Fatalf("recovered spec = %q", recovered.Specs[service.Hostname])
	}

	if status := recovered.APIs[service.Hostname]; status.Failure != "" || !status.LastSuccess.After(firstSuccess) {
		t.Fatalf("recovered API status = %+v", status)
	}

	if recovered.APIs[service.Hostname].Generation != firstGeneration {
		t.Fatalf("ordinary service update changed generation: %+v", recovered.APIs[service.Hostname])
	}

	d.updateServices(service, models.EventDelete)

	deleted := d.Snapshot()
	if _, ok := deleted.APIs[service.Hostname]; ok {
		t.Fatalf("deleted API status retained: %+v", deleted.APIs)
	}

	if _, ok := deleted.Specs[service.Hostname]; ok {
		t.Fatalf("deleted API spec retained: %+v", deleted.Specs)
	}

	mu.Lock()
	failure = "spec_fetch_failed"
	mu.Unlock()
	d.updateServices(service, models.EventAdd)

	recreated := d.Snapshot()
	if _, ok := recreated.Specs[service.Hostname]; ok {
		t.Fatalf("failed recreated API inherited deleted specification: %q", recreated.Specs[service.Hostname])
	}

	if status := recreated.APIs[service.Hostname]; status.Failure != "spec_fetch_failed" || status.Generation <= firstGeneration || !status.LastSuccess.IsZero() {
		t.Fatalf("recreated API inherited old status: %+v", status)
	}
}

func TestSnapshotDoesNotResurrectAPIAfterInFlightFetchAndDelete(t *testing.T) {
	d := newSnapshotTestDiscoverer()
	service := &models.Service{
		Hostname: "api.example.test",
		Ports:    models.PortList{{Protocol: models.ProtocolHTTP, Port: 8080}},
	}
	d.data.services.Insert(cloneService(service))
	d.apis[service.Hostname] = APIStatus{}
	d.serviceRevisions[service.Hostname] = 1

	entered := make(chan struct{})
	release := make(chan struct{})

	var fetchOnce sync.Once

	d.fetch = func(services []*models.Service) (map[string][]byte, map[string]string) {
		if len(services) == 0 {
			return map[string][]byte{}, map[string]string{}
		}

		fetchOnce.Do(func() {
			close(entered)
			<-release
		})

		return map[string][]byte{service.Hostname: []byte("late-spec")}, nil
	}

	fetchDone := make(chan struct{})
	go func() {
		d.discover()
		close(fetchDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("fetch did not start")
	}

	deleteDone := make(chan struct{})
	go func() {
		d.updateServices(service, models.EventDelete)
		close(deleteDone)
	}()

	deadline := time.After(time.Second)

	for {
		snapshot := d.Snapshot()
		if len(snapshot.APIs) == 0 && len(snapshot.Specs) == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("delete did not remove API state while fetch was in flight")
		case <-time.After(time.Millisecond):
		}
	}

	close(release)
	select {
	case <-fetchDone:
	case <-time.After(time.Second):
		t.Fatal("in-flight fetch did not finish")
	}
	select {
	case <-deleteDone:
	case <-time.After(time.Second):
		t.Fatal("delete refresh did not finish")
	}

	snapshot := d.Snapshot()
	if len(snapshot.APIs) != 0 || len(snapshot.Specs) != 0 {
		t.Fatalf("deleted API was resurrected: %+v", snapshot)
	}
}

func TestChangeCallbackCanReadSnapshot(t *testing.T) {
	d := newSnapshotTestDiscoverer()
	callbackDone := make(chan struct{})

	var callbackOnce sync.Once

	d.RegisterOnChangeFunc(func() {
		callbackOnce.Do(func() {
			go func() {
				_ = d.Snapshot()

				close(callbackDone)
			}()
			select {
			case <-callbackDone:
			case <-time.After(time.Second):
				t.Error("change callback ran while discovery state lock was held")
			}
		})
	})

	d.fetch = func(_ []*models.Service) (map[string][]byte, map[string]string) {
		return map[string][]byte{}, map[string]string{"api.example.test": "spec_fetch_failed"}
	}

	d.updateServices(&models.Service{
		Hostname: "api.example.test",
		Ports:    models.PortList{{Protocol: models.ProtocolHTTP, Port: 8080}},
	}, models.EventAdd)
}

func TestRunDistinguishesSyncedEmptyInventoryAndShutdownIsIdempotent(t *testing.T) {
	watcher := &snapshotTestWatcher{}
	d := newSnapshotTestDiscoverer()
	d.services = watcher
	d.synced = false
	d.stop = make(chan struct{})

	d.Run()

	if snapshot := d.Snapshot(); snapshot.Synced {
		t.Fatal("snapshot reported synchronization before informer readiness")
	}

	watcher.setReady(true)
	waitForSnapshot(t, d, func(snapshot Snapshot) bool { return snapshot.Synced })

	snapshot := d.Snapshot()
	if len(snapshot.Specs) != 0 || len(snapshot.APIs) != 0 {
		t.Fatalf("empty synchronized inventory has unexpected APIs: %+v", snapshot)
	}

	if snapshot.Revision == 0 {
		t.Fatal("synchronization did not advance snapshot revision")
	}

	d.Shutdown()
	d.Shutdown()
}

func TestRunKeepsUnsynchronizedInventoryUnavailable(t *testing.T) {
	watcher := &snapshotTestWatcher{}
	d := newSnapshotTestDiscoverer()
	d.services = watcher
	d.synced = false
	d.stop = make(chan struct{})

	d.Run()
	time.Sleep(30 * time.Millisecond)

	if snapshot := d.Snapshot(); snapshot.Synced {
		t.Fatal("snapshot reported an unsynchronized inventory as empty and ready")
	}

	d.Shutdown()
}

func TestShutdownCancelsSpecFetch(t *testing.T) {
	oldTimeout := viper.GetDuration(config.DiscoverySpecLoadTimeout)
	oldRewrites := viper.GetString(config.SpecRewrites)
	viper.Set(config.DiscoverySpecLoadTimeout, time.Minute)
	viper.Set(config.SpecRewrites, "")

	defer viper.Set(config.DiscoverySpecLoadTimeout, oldTimeout)
	defer viper.Set(config.SpecRewrites, oldRewrites)

	for _, blockBody := range []bool{false, true} {
		t.Run(fmt.Sprintf("block_body_%t", blockBody), func(t *testing.T) {
			entered := make(chan struct{})
			canceled := make(chan struct{})
			release := make(chan struct{})

			var requests atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					if blockBody {
						w.WriteHeader(http.StatusOK)
						w.(http.Flusher).Flush()
					}

					close(entered)
					select {
					case <-r.Context().Done():
						close(canceled)
					case <-release:
					}
				}
			}))
			defer server.Close()
			defer close(release)

			host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}

			port, err := strconv.Atoi(portText)
			if err != nil {
				t.Fatal(err)
			}

			service := &models.Service{
				Hostname: host,
				Ports: models.PortList{
					{Protocol: models.ProtocolHTTP, Port: port},
					{Protocol: models.ProtocolHTTP, Port: port},
				},
			}
			d := newSnapshotTestDiscoverer()
			d.services = &snapshotTestWatcher{ready: true}
			d.data.services.Insert(service)
			d.apis[host] = APIStatus{}
			d.Run()

			defer d.Shutdown()

			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("discovery did not start fetching")
			}

			before := d.Snapshot()
			done := make(chan struct{})

			go func() {
				d.Shutdown()
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(time.Second):
				// Release the blocked handler before deferred shutdown waits.
				server.CloseClientConnections()
				t.Fatal("shutdown did not cancel the in-flight specification fetch")
			}

			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("specification request context was not canceled")
			}

			if got := requests.Load(); got != 1 {
				t.Fatalf("shutdown fetched remaining ports: got %d requests, want 1", got)
			}

			if after := d.Snapshot(); after.Revision != before.Revision || after.APIs[host] != before.APIs[host] {
				t.Fatalf("shutdown published a canceled refresh: before %+v, after %+v", before, after)
			}
		})
	}
}

func newSnapshotTestDiscoverer() *Discoverer {
	return &Discoverer{
		data:             &state{services: models.NewServiceMap()},
		stop:             make(chan struct{}),
		specs:            make(map[string][]byte),
		apis:             make(map[string]APIStatus),
		serviceRevisions: make(map[string]uint64),
		serviceEpochs:    make(map[string]uint64),
		synced:           true,
		notify:           func() {},
	}
}

func waitForSnapshot(t *testing.T, d *Discoverer, predicate func(Snapshot) bool) {
	t.Helper()

	deadline := time.After(time.Second)

	for {
		if predicate(d.Snapshot()) {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for discovery snapshot")
		case <-time.After(time.Millisecond):
		}
	}
}

type snapshotTestWatcher struct {
	mu    sync.Mutex
	ready bool
}

func (w *snapshotTestWatcher) AppendServiceHandler(func(*models.Service, models.Event))       {}
func (w *snapshotTestWatcher) AppendDeploymentHandler(func(*models.Deployment, models.Event)) {}
func (w *snapshotTestWatcher) Run(stop <-chan struct{}) {
	<-stop
}

func (w *snapshotTestWatcher) HasSynced() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.ready
}

func (w *snapshotTestWatcher) setReady(ready bool) {
	w.mu.Lock()
	w.ready = ready
	w.mu.Unlock()
}
