package handlers

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover"
)

// Updater periodically refreshes API documentation from discovered specs.
type Updater struct {
	d discover.DiscoveryManager
	r *mux.Router

	ticker *time.Ticker
	done   chan bool

	closed   bool
	notified bool
}

// NewAutoDiscoverUpdater creates a new Updater instance with AutoDiscovery background process.
func NewAutoDiscoverUpdater(discoverer discover.DiscoveryManager) *Updater {
	router := createMiddlewareRouter()

	// create updater instance to periodically fetch the latest auto-discovered specs
	updater := &Updater{
		d:        discoverer,
		r:        router,
		ticker:   time.NewTicker(viper.GetDuration(config.DiscoveryPeriodTime)),
		done:     make(chan bool),
		notified: true,
	}

	// register the an OnChange function to know when the available discovery data has been changed
	discoverer.RegisterOnChangeFunc(updater.onChange)

	// wait a short configured period of time and then
	time.AfterFunc(viper.GetDuration(config.DiscoveryInitialDelay), updater.update)

	// initiate periodic spec updater to fetch latest discovered API specs and generate API documentation
	go func(u *Updater) {
		for {
			select {
			case <-u.done:
				return
			case <-u.ticker.C:
				u.update()
			}
		}
	}(updater)

	return updater
}

// Router returns an Updater's Router instance.
func (u *Updater) Router() http.Handler {
	return u.r
}

// Close stops the periodic ticker and closes boolean channel.
func (u *Updater) Close() {
	if u.closed {
		return
	}

	u.ticker.Stop()

	u.done <- true
	close(u.done)

	u.closed = true
}

func (u *Updater) onChange() {
	u.notified = true
}

func (u *Updater) update() {
	if !u.notified {
		return
	}

	loadAndRegisterSpecs(u.r, u.d)
	u.notified = false
}
