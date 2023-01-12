package discover

import (
	"sync"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover/models"
)

// Discoverer represents the state of the discovery mechanism.
type Discoverer struct {
	services watcher

	sLock sync.Mutex

	data *state
	stop chan struct{}

	specs map[string][]byte

	notify func()
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
		stop:   make(chan struct{}),
		specs:  make(map[string][]byte),
		notify: func() {},
	}

	// register handlers; ignore errors as it will always return nil
	d.services.AppendServiceHandler(d.updateServices)
	d.services.AppendDeploymentHandler(d.updateDeployments)

	return d, nil
}

// Shutdown safely stops Discovery process.
func (d *Discoverer) Shutdown() {
	close(d.stop)
	log().Info("shutting down discovery process")
}

// Run starts the discovery process.
func (d *Discoverer) Run() {
	log().Info("starting discovery process")

	go d.services.Run(d.stop)

	d.discover()
}

// RegisterOnChangeFunc provides a way to notifier a consumer of the Specs that data has changed instead of constantly checking.
func (d *Discoverer) RegisterOnChangeFunc(f func()) {
	d.notify = f
}

// Specs returns discovered API specs.
func (d *Discoverer) Specs() map[string][]byte {
	d.sLock.Lock()
	defer d.sLock.Unlock()

	return d.specs
}

func (d *Discoverer) discover() {
	d.sLock.Lock()
	defer d.sLock.Unlock()

	// fetch API specs from services and process the necessary
	// API changes to meet documentation requirements
	//  - remove private APIs and Methods
	//  - set necessary extensions for dapperdox
	//  - rewrite spec details for Schema, Security Definitions, Security
	specs := d.fetchAPISpecs()
	if len(specs) == 0 {
		return
	}

	log().Infof("successfully processed [%d] API specs", len(specs))

	// update local cache with latest service specs
	d.specs = specs

	d.notify()
}

func (d *Discoverer) updateServices(s *models.Service, e models.Event) {
	log().Debugf("(Discover Handler) Service: %v Event: %v", s, e)

	if isIgnoredSvc(s.Hostname) {
		log().Debugf("(Discover Handler) skipping service is part of ignore list : %s", s.Hostname)

		return
	}

	switch e {
	case models.EventAdd, models.EventUpdate:
		d.data.services.Insert(s)
	case models.EventDelete:
		d.data.services.Delete(s)
	}

	d.discover()
}

func (d *Discoverer) updateDeployments(dpl *models.Deployment, e models.Event) {
	log().Debugf("(Discover Handler) Deployment: %v Event: %v", *dpl, e)

	if isIgnoredSvc(dpl.Name) {
		// if the deployment that triggered the update is on the ignore list; return
		log().Debugf("(Discover Handler) skipping deployment that is part of ignore list : %+v", *dpl)

		return
	}

	d.discover()
}
