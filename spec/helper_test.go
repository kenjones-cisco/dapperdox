package spec

import (
	"github.com/go-openapi/swag"
)

type fakeDiscoverer struct {
	specs map[string][]byte
}

// Shutdown no-op testing method.
func (fd *fakeDiscoverer) Shutdown() {}

// Run no-op testing method.
func (fd *fakeDiscoverer) Run() {}

// Specs returns the configured specs populated during pre-test initialization.
func (fd *fakeDiscoverer) Specs() map[string][]byte {
	return fd.specs
}

// specToByteSlice opens a test spec at a provided file-path and converts into a byte slice.
func specToByteSlice(specLoc string) []byte {
	raw, err := swag.LoadFromFileOrHTTP(specLoc)
	if err != nil {
		log().Errorf("cannot load spec %v", err)

		return nil
	}

	return raw
}
