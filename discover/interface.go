package discover

// DiscoveryManager Default Object for discoverer manager.
type DiscoveryManager interface {
	// Shutdown safely stops Discovery process.
	Shutdown()
	// Run starts the discovery process.
	Run()
	// Specs returns the cached instances of discovered specs.
	Specs() map[string][]byte
}
