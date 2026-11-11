// Package discover provides a mechanism for discovering running services
// within Kubernetes and their API specifications.
package discover

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/spf13/viper"
	"k8s.io/client-go/kubernetes"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover/models"
)

const (
	discovererWorkerCount   = 2
	initialSyncPollInterval = 20 * time.Millisecond
)

// Discoverer represents the state of the discovery mechanism.
type Discoverer struct {
	fetchCtx         context.Context //nolint:containedctx // Discovery owns this context and cancels it on shutdown.
	cancelFetch      context.CancelFunc
	services         watcher
	notify           func()
	data             *state
	stop             chan struct{}
	fetch            func([]*models.Service) (map[string][]byte, map[string]string)
	specs            map[string][]byte
	apis             map[string]APIStatus
	serviceRevisions map[string]uint64
	serviceEpochs    map[string]uint64
	runWG            sync.WaitGroup
	serviceRevision  uint64
	nextServiceEpoch uint64
	revision         uint64
	mu               sync.RWMutex
	refreshMu        sync.Mutex
	lifecycleMu      sync.Mutex
	synced           bool
	started          bool
	stopped          bool
}

type state struct {
	services models.ServiceMap
}

// NewDiscoverer configures a new instance of a Discoverer using Kubernetes client.
func NewDiscoverer() (DiscoveryManager, error) {
	log().Info("initializing new discoverer instance")

	client, err := newClient()
	if err != nil {
		return nil, err
	}

	return newDiscovererWithClient(client), nil
}

func newDiscovererWithClient(client kubernetes.Interface) DiscoveryManager {
	options := catalogOptions{
		DomainSuffix:     viper.GetString(config.DiscoverySuffix),
		WatchedNamespace: viper.GetString(config.DiscoveryNamespace),
		ResyncPeriod:     viper.GetDuration(config.DiscoveryInterval),
	}

	d := &Discoverer{
		services: newCatalog(client, options),
		data: &state{
			services: models.NewServiceMap(),
		},
		stop:             make(chan struct{}),
		specs:            make(map[string][]byte),
		apis:             make(map[string]APIStatus),
		serviceRevisions: make(map[string]uint64),
		serviceEpochs:    make(map[string]uint64),
		notify:           func() {},
	}

	// register handlers; ignore errors as it will always return nil
	d.services.AppendServiceHandler(d.updateServices)
	d.services.AppendDeploymentHandler(d.updateDeployments)

	return d
}

// Shutdown safely stops Discovery process. It is safe to call repeatedly.
func (d *Discoverer) Shutdown() {
	d.lifecycleMu.Lock()
	if !d.stopped {
		d.stopped = true
		if d.cancelFetch != nil {
			d.cancelFetch()
		}

		if d.stop != nil {
			close(d.stop)
		}
	}
	d.lifecycleMu.Unlock()

	d.runWG.Wait()
	log().Info("shutting down discovery process")
}

func (d *Discoverer) fetchContext() context.Context {
	d.lifecycleMu.Lock()
	defer d.lifecycleMu.Unlock()

	if d.fetchCtx == nil {
		d.fetchCtx, d.cancelFetch = context.WithCancel(context.Background())
	}

	if d.stopped {
		d.cancelFetch()
	}

	return d.fetchCtx
}

// Run starts the discovery process and waits for initial informer
// synchronization or shutdown.
func (d *Discoverer) Run() {
	d.lifecycleMu.Lock()
	if d.started || d.stopped {
		d.lifecycleMu.Unlock()

		return
	}

	if d.stop == nil {
		d.stop = make(chan struct{})
	}

	d.started = true
	d.runWG.Add(discovererWorkerCount)
	stop := d.stop
	services := d.services
	d.lifecycleMu.Unlock()

	go func() {
		defer d.runWG.Done()

		if services != nil {
			services.Run(stop)
		}
	}()

	go func() {
		defer d.runWG.Done()
		d.waitForInitialSync(stop)
	}()
}

func (d *Discoverer) waitForInitialSync(stop <-chan struct{}) {
	ready, ok := d.services.(interface{ HasSynced() bool })
	if !ok {
		return
	}

	ticker := time.NewTicker(initialSyncPollInterval)
	defer ticker.Stop()

	for {
		if ready.HasSynced() {
			d.setSynced()
			d.discover()

			return
		}

		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

// RegisterOnChangeFunc provides a way to notify a consumer when discovery
// state changes instead of constantly checking it.
func (d *Discoverer) RegisterOnChangeFunc(f func()) {
	if f == nil {
		f = func() {}
	}

	d.mu.Lock()
	d.notify = f
	d.mu.Unlock()
}

// Snapshot returns an immutable copy of the current discovery state.
func (d *Discoverer) Snapshot() Snapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return Snapshot{
		Specs:    cloneSpecs(d.specs),
		APIs:     cloneAPIStatuses(d.apis),
		Synced:   d.synced,
		Revision: d.revision,
	}
}

// Specs returns the cached instances of discovered specs.
func (d *Discoverer) Specs() map[string][]byte {
	return d.Snapshot().Specs
}

func (d *Discoverer) setSynced() {
	d.mu.Lock()
	if d.synced {
		d.mu.Unlock()

		return
	}

	d.synced = true
	d.revision++
	notify := d.notify
	d.mu.Unlock()

	if notify != nil {
		notify()
	}
}

func (d *Discoverer) discover() {
	d.refreshMu.Lock()
	defer d.refreshMu.Unlock()

	if d.isStopping() {
		return
	}

	d.mu.RLock()
	if !d.synced {
		d.mu.RUnlock()

		return
	}

	services := d.data.services.List()
	serviceRevisions := make(map[string]uint64, len(d.serviceRevisions))
	active := make(map[string]struct{}, len(d.apis))

	for hostname, revision := range d.serviceRevisions {
		serviceRevisions[hostname] = revision
	}

	for hostname := range d.apis {
		active[hostname] = struct{}{}
	}
	d.mu.RUnlock()

	fetch := d.fetch
	ctx := d.fetchContext()

	if fetch == nil {
		fetch = func(services []*models.Service) (map[string][]byte, map[string]string) {
			return fetchAPIResults(ctx, services)
		}
	}

	specs, failures := fetch(services)

	if ctx.Err() != nil {
		return
	}

	now := time.Now().UTC()

	d.mu.Lock()
	changed := false

	for hostname := range active {
		if _, stillActive := d.apis[hostname]; !stillActive {
			continue
		}

		if d.serviceRevisions[hostname] != serviceRevisions[hostname] {
			continue
		}

		if failure, failed := failures[hostname]; failed {
			status := d.apis[hostname]
			if status.Failure != failure {
				status.Failure = failure
				d.apis[hostname] = status
				changed = true
			}

			continue
		}

		data, succeeded := specs[hostname]
		if !succeeded {
			continue
		}

		status := d.apis[hostname]
		status.Failure = ""
		status.LastSuccess = now

		if !bytes.Equal(d.specs[hostname], data) {
			d.specs[hostname] = append([]byte(nil), data...)
			changed = true
		}

		if d.apis[hostname] != status {
			d.apis[hostname] = status
			changed = true
		}
	}

	var notify func()

	if changed {
		d.revision++
		notify = d.notify
	}
	d.mu.Unlock()

	if notify != nil {
		notify()
	}
}

func (d *Discoverer) isStopping() bool {
	d.lifecycleMu.Lock()
	stopped := d.stopped
	stop := d.stop
	d.lifecycleMu.Unlock()

	if stopped {
		return true
	}

	if stop == nil {
		return false
	}
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

func (d *Discoverer) updateServices(service *models.Service, event models.Event) {
	log().Debugf("(Discover Handler) Service: %v Event: %v", service, event)

	if service == nil || isIgnoredSvc(service.Hostname) {
		if service != nil {
			log().Debugf("(Discover Handler) skipping service is part of ignore list: %s", service.Hostname)
		}

		return
	}

	var notify func()

	inventoryChanged := false

	d.mu.Lock()
	d.ensureStateLocked()

	switch event {
	case models.EventAdd, models.EventUpdate:
		inventoryChanged = true
		svc := cloneService(service)
		d.data.services.Insert(svc)

		if _, exists := d.serviceEpochs[svc.Hostname]; !exists {
			d.nextServiceEpoch++
			d.serviceEpochs[svc.Hostname] = d.nextServiceEpoch
		}

		d.serviceRevision++
		d.serviceRevisions[svc.Hostname] = d.serviceRevision
		d.updateActiveAPIsLocked(svc.Hostname, serviceAPIKeys(svc))
	case models.EventDelete:
		inventoryChanged = true

		d.data.services.Delete(service)

		d.serviceRevision++
		delete(d.serviceRevisions, service.Hostname)
		delete(d.serviceEpochs, service.Hostname)
		d.removeAPIStatusLocked(service.Hostname)
	}

	if inventoryChanged {
		d.revision++
		notify = d.notify
	}
	d.mu.Unlock()

	if notify != nil {
		notify()
	}

	d.discover()
}

func (d *Discoverer) updateDeployments(deployment *models.Deployment, event models.Event) {
	log().Debugf("(Discover Handler) Deployment: %v Event: %v", deployment, event)

	if deployment != nil && isIgnoredSvc(deployment.Name) {
		log().Debugf("(Discover Handler) skipping deployment that is part of ignore list: %+v", *deployment)

		return
	}

	d.discover()
}

func (d *Discoverer) updateActiveAPIsLocked(hostname string, keys map[string]struct{}) bool {
	changed := false

	for key := range d.apis {
		if key == hostname {
			if _, keep := keys[key]; !keep {
				delete(d.apis, key)
				delete(d.specs, key)

				changed = true
			}
		}
	}

	for key := range keys {
		if _, exists := d.apis[key]; !exists {
			d.apis[key] = APIStatus{Generation: d.serviceEpochs[key]}
			changed = true
		}
	}

	return changed
}

func (d *Discoverer) ensureStateLocked() {
	if d.data == nil {
		d.data = &state{services: models.NewServiceMap()}
	} else if d.data.services == nil {
		d.data.services = models.NewServiceMap()
	}

	if d.specs == nil {
		d.specs = make(map[string][]byte)
	}

	if d.apis == nil {
		d.apis = make(map[string]APIStatus)
	}

	if d.serviceRevisions == nil {
		d.serviceRevisions = make(map[string]uint64)
	}

	if d.serviceEpochs == nil {
		d.serviceEpochs = make(map[string]uint64)
	}
}

func (d *Discoverer) removeAPIStatusLocked(hostname string) bool {
	_, statusExists := d.apis[hostname]
	_, specExists := d.specs[hostname]
	delete(d.apis, hostname)
	delete(d.specs, hostname)

	return statusExists || specExists
}

func serviceAPIKeys(service *models.Service) map[string]struct{} {
	keys := make(map[string]struct{})
	if service == nil || service.Hostname == "" || isIgnoredSvc(service.Hostname) {
		return keys
	}

	for _, port := range service.Ports {
		if port != nil && port.Protocol.IsHTTP() {
			keys[service.Hostname] = struct{}{}

			break
		}
	}

	return keys
}

func cloneService(service *models.Service) *models.Service {
	serviceCopy := *service
	serviceCopy.Ports = make(models.PortList, len(service.Ports))

	for i, port := range service.Ports {
		if port == nil {
			continue
		}

		portCopy := *port
		serviceCopy.Ports[i] = &portCopy
	}

	return &serviceCopy
}

func cloneSpecs(specs map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(specs))
	for key, data := range specs {
		result[key] = append([]byte(nil), data...)
	}

	return result
}

func cloneAPIStatuses(apis map[string]APIStatus) map[string]APIStatus {
	result := make(map[string]APIStatus, len(apis))
	for key, status := range apis {
		result[key] = status
	}

	return result
}
