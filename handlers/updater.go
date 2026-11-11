package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover"
	log "github.com/kenjones-cisco/dapperdox/logger"
	"github.com/kenjones-cisco/dapperdox/render"
	"github.com/kenjones-cisco/dapperdox/render/asset"
	"github.com/kenjones-cisco/dapperdox/spec"
)

type generation struct {
	router *mux.Router
	routes int
}

const stateStale = "stale"

const stateUnavailable = "unavailable"

var errDiscoveryPeriod = errors.New("discovery update period must be positive")

type acceptedAPI struct {
	parsed   *spec.APISpecification
	raw      []byte
	instance uint64
}

// APIStatus contains only sanitized current state, not an error history.
type APIStatus struct {
	Name        string    `json:"name"`
	State       string    `json:"state"`
	LastSuccess time.Time `json:"last_success"`
	Failure     string    `json:"failure,omitempty"`
}

type publishedView struct {
	generation *generation
	statuses   map[string]render.Status
	status     Status
}

// Status is the bounded operator view of the current catalog and last build.
type Status struct {
	Published   time.Time            `json:"published_at"`
	LastAttempt time.Time            `json:"last_attempt"`
	APIs        map[string]APIStatus `json:"apis"`
	Failure     string               `json:"failure,omitempty"`
	Revision    uint64               `json:"source_revision"`
	Generation  uint64               `json:"generation"`
	Routes      int                  `json:"routes"`
	GFMRules    int                  `json:"gfm_rules"`
	Assets      int                  `json:"assets"`
	Ready       bool                 `json:"ready"`
	Synced      bool                 `json:"synced"`
	Building    bool                 `json:"building"`
}

// Updater owns the sole builder and atomically publishes complete request views.
type Updater struct {
	d        discover.DiscoveryManager
	assets   *asset.Catalog
	current  atomic.Pointer[publishedView]
	last     map[string]acceptedAPI
	stop     chan struct{}
	done     chan struct{}
	close    sync.Once
	buildMu  sync.Mutex
	dirty    atomic.Bool
	building atomic.Bool
}

// NewAutoDiscoverUpdater starts a single, coalescing refresh worker.
func NewAutoDiscoverUpdater(d discover.DiscoveryManager) (*Updater, error) {
	catalog, err := asset.NewCatalog()
	if err != nil {
		return nil, err
	}

	period := viper.GetDuration(config.DiscoveryPeriodTime)
	if period <= 0 {
		return nil, errDiscoveryPeriod
	}

	u := &Updater{
		d: d, assets: catalog, last: make(map[string]acceptedAPI),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	u.current.Store(&publishedView{status: Status{APIs: make(map[string]APIStatus)}})
	u.onChange()
	d.RegisterOnChangeFunc(u.onChange)

	go u.run(viper.GetDuration(config.DiscoveryInitialDelay), period)

	return u, nil
}

// Router is a stable dispatcher. Each request pins exactly one published view.
func (u *Updater) Router() http.Handler { return u }

func (u *Updater) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	view := u.current.Load()

	switch req.URL.Path {
	case "/healthz":
		w.WriteHeader(http.StatusOK)

		return
	case "/readyz":
		if !view.status.Ready {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		return
	}

	if view.generation == nil {
		http.Error(w, "documentation initializing", http.StatusServiceUnavailable)

		return
	}

	view.generation.router.ServeHTTP(w, render.WithStatus(req, view.statuses))
}

// ManagementHandler is deliberately not mounted on the documentation listener.
func (u *Updater) ManagementHandler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /status", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")

		status := u.current.Load().status
		status.Building = u.building.Load()

		if err := json.NewEncoder(w).Encode(status); err != nil {
			log.Logger().ErrorContext(req.Context(), "encode documentation status", slog.Any("error", err))
		}
	})

	return router
}

// Close stops owned work and is safe to call concurrently or repeatedly.
func (u *Updater) Close() {
	u.close.Do(func() { close(u.stop) })
	<-u.done
}

func (u *Updater) run(delay, period time.Duration) {
	defer close(u.done)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-u.stop:
		return
	case <-timer.C:
		u.update()
	}

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for {
		select {
		case <-u.stop:
			return
		case <-ticker.C:
			u.update()
		}
	}
}

func (u *Updater) onChange() { u.dirty.Store(true) }

func (u *Updater) snapshot() discover.Snapshot {
	if source, ok := u.d.(interface{ Snapshot() discover.Snapshot }); ok {
		return source.Snapshot()
	}

	raw := u.d.Specs()
	apis := make(map[string]discover.APIStatus, len(raw))
	now := time.Now().UTC()

	for key := range raw {
		apis[key] = discover.APIStatus{LastSuccess: now}
	}

	return discover.Snapshot{Specs: raw, APIs: apis, Synced: true}
}

func reconcileAPI(key string, data []byte, fetched bool, outcome discover.APIStatus, old acceptedAPI, prior APIStatus) (acceptedAPI, APIStatus) {
	api := old
	failure := outcome.Failure

	if failure == "" && !fetched {
		failure = stateUnavailable
	}

	if failure == "" && (old.parsed == nil || !bytes.Equal(old.raw, data)) {
		value, err := spec.Parse("/"+strings.TrimPrefix(key, "/")+"/api.json", data)
		if err != nil {
			failure = "invalid_specification"

			log.Logger().With(slog.Any("api", key)).Warn("invalid API specification; retaining last known-good version")
		} else {
			api = acceptedAPI{raw: bytes.Clone(data), parsed: value, instance: outcome.Generation}
		}
	}

	status := APIStatus{State: "available", LastSuccess: outcome.LastSuccess}
	if api.parsed != nil {
		status.Name = api.parsed.APIInfo.Title
	}

	if failure != "" {
		status.Failure = failure
		status.State = stateUnavailable
		status.LastSuccess = prior.LastSuccess

		if old.parsed != nil {
			status.State = stateStale
		}
	}

	return api, status
}

func (u *Updater) update() {
	u.buildMu.Lock()
	defer u.buildMu.Unlock()

	if !u.dirty.Swap(false) {
		return
	}

	u.building.Store(true)

	defer u.building.Store(false)

	previous := u.current.Load()
	snapshot := u.snapshot()
	next := &publishedView{
		generation: previous.generation,
		statuses:   make(map[string]render.Status),
		status: Status{
			Ready: previous.status.Ready, Synced: snapshot.Synced,
			Revision: snapshot.Revision, Generation: previous.status.Generation,
			Published: previous.status.Published, LastAttempt: time.Now().UTC(),
			Routes: previous.status.Routes, APIs: make(map[string]APIStatus),
			GFMRules: u.assets.RuleCount(), Assets: len(u.assets.Names()),
		},
	}

	if !snapshot.Synced {
		next.status.Failure = "discovery_not_synchronized"
		next.status.APIs = previous.status.APIs
		next.statuses = previous.statuses
		u.current.Store(next)
		u.dirty.Store(true)

		return
	}

	accepted, changed := u.reconcile(snapshot, previous, next)

	if changed {
		var err error
		if next.generation, err = u.buildAccepted(accepted); err != nil {
			log.Logger().With(slog.Any("error", err)).Error("documentation generation rejected")

			rejected := *previous
			rejected.status = previous.status
			rejected.status.Failure = "generation_build_failed"
			rejected.status.LastAttempt = next.status.LastAttempt
			u.current.Store(&rejected)
			u.dirty.Store(true)

			return
		}

		next.status.Generation++
		next.status.Published = time.Now().UTC()
		next.status.Routes = next.generation.routes
	}

	if snapshot.Revision != 0 && u.snapshot().Revision != snapshot.Revision {
		u.dirty.Store(true)

		return
	}

	next.status.Ready = true
	for key, status := range next.status.APIs {
		if old := previous.status.APIs[key]; old.State != status.State || old.Failure != status.Failure {
			log.Logger().With(slog.Any("api", key)).With(slog.Any("state", status.State)).
				With(slog.Any("failure", status.Failure)).Info("documentation availability changed")
		}
	}

	u.last = accepted
	u.current.Store(next)
}

func (u *Updater) reconcile(snapshot discover.Snapshot, previous, next *publishedView) (accepted map[string]acceptedAPI, changed bool) {
	accepted = make(map[string]acceptedAPI)
	changed = previous.generation == nil

	for key, outcome := range snapshot.APIs {
		old, hadOld := u.last[key]
		prior := previous.status.APIs[key]

		if hadOld && old.instance != outcome.Generation {
			old = acceptedAPI{}
			hadOld = false
			prior = APIStatus{}
			changed = true
		}

		data, fetched := snapshot.Specs[key]

		api, status := reconcileAPI(key, data, fetched, outcome, old, prior)
		if api.parsed != nil {
			accepted[key] = api
			next.statuses[api.parsed.ID] = render.Status{
				State: status.State, Name: status.Name, LastSuccess: status.LastSuccess,
			}

			if !hadOld || !bytes.Equal(old.raw, api.raw) {
				changed = true
			}
		}

		next.status.APIs[key] = status
	}

	return accepted, changed || len(accepted) != len(u.last)
}

func (u *Updater) buildAccepted(accepted map[string]acceptedAPI) (*generation, error) {
	parsed := make(map[string]*spec.APISpecification, len(accepted))
	raw := make(map[string][]byte, len(accepted))

	for key, api := range accepted {
		parsed[key], raw[key] = api.parsed, api.raw
	}

	suite, err := spec.NewSuite(parsed)
	if err != nil {
		return nil, err
	}

	return buildGeneration(suite, raw, u.assets)
}
