package discover

import (
	"sync"

	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/util/sets"

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
}

type state struct {
	services    *models.ServiceMap
	deployments *models.DeploymentMap
}

// NewDiscoverer configures a new instance of a Discoverer using Kubernetes client.
func NewDiscoverer() (DiscoveryManager, error) {
	log().Info("initializing new discoverer instance")

	client, err := newClient()
	if err != nil {
		return nil, err
	}

	ctlg := newCatalog(client, catalogOptions{
		DomainSuffix:     viper.GetString(config.DiscoverySuffix),
		WatchedNamespace: viper.GetString(config.DiscoveryNamespace),
		ResyncPeriod:     viper.GetDuration(config.DiscoveryInterval),
	})

	sm := models.NewServiceMap()
	dm := models.NewDeploymentMap()

	d := &Discoverer{
		services: ctlg,
		data: &state{
			services:    &sm,
			deployments: &dm,
		},
		stop:  make(chan struct{}),
		specs: make(map[string][]byte),
	}

	// register handlers; ignore errors as it will always return nil
	ctlg.AppendServiceHandler(d.updateServices)
	ctlg.AppendDeploymentHandler(d.updateDeployments)

	return d, nil
}

// Shutdown safely stops Discovery process.
func (d *Discoverer) Shutdown() {
	close(d.stop)
	log().Info("shutting down discovery process")
}

// Run starts the discovery process.
func (d *Discoverer) Run() {
	d.discover()

	if d.services != nil {
		go d.services.Run(d.stop)
	}
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

	log().Debug("successfully processed API changes")

	// update local cache with latest service specs
	d.specs = specs
}

func (d *Discoverer) updateServices(s *models.Service, e models.Event) {
	log().Debugf("(Discover Handler) Service: %v Event: %v", s, e)

	if sets.NewString(viper.GetStringSlice(config.DiscoveryServiceIgnoreList)...).Has(s.Hostname) {
		log().Debugf("%v service is ignored", s.Hostname)

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
	log().Debugf("(Discover Handler) Deployment: %v Event: %v", dpl, e)

	switch e {
	case models.EventAdd, models.EventUpdate:
		d.data.deployments.Insert(dpl)
	case models.EventDelete:
		d.data.deployments.Delete(dpl)
	}

	d.discover()
}
