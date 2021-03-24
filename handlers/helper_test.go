package handlers

import (
	"bytes"
	"testing"

	"github.com/go-openapi/swag"

	log "github.com/kenjones-cisco/dapperdox/logger"
)

type fakeDiscover struct {
	t *testing.T

	sPaths    map[string]string
	wantSpecs map[string][]byte
	wantErr   bool
}

// Shutdown no-op testing method.
func (fd *fakeDiscover) Shutdown() {}

// Run no-op testing method.
func (fd *fakeDiscover) Run() {}

// Specs inspects testing conditions for determining auto-discovery updater properly fetches specs.
func (fd *fakeDiscover) Specs() map[string][]byte {
	specs := make(map[string][]byte)

	for k, v := range fd.sPaths {
		got := specToByteSlice(v)
		want := fd.wantSpecs[k]

		if res := bytes.Compare(got, want); res != 0 && !fd.wantErr {
			fd.t.Errorf("Specs()\n\tgot=%s\n\twant=%s", string(got), string(want))

			return nil
		}
	}

	return specs
}

// specToByteSlice opens a test spec at a provided file-path and converts into a byte slice.
func specToByteSlice(specLoc string) []byte {
	raw, err := swag.LoadFromFileOrHTTP(specLoc)
	if err != nil {
		log.Logger().Errorf("cannot load spec %v", err)

		return nil
	}

	return raw
}
