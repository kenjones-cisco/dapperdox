package discover

type defaultDiscoverer struct{}

// NewDefaultDiscoverer Default Object for client manager.
func NewDefaultDiscoverer() DiscoveryManager {
	return &defaultDiscoverer{}
}

// Shutdown safely stops Discovery process.
func (d *defaultDiscoverer) Shutdown() {
	log().Info("Discoverer not implemented")
}

// Run starts the discovery process.
func (d *defaultDiscoverer) Run() {
	log().Info("Discoverer not implemented")
}

// Specs returns the cached instances of discovered specs.
func (d *defaultDiscoverer) Specs() map[string][]byte {
	log().Info("Discoverer not implemented")

	return nil
}
