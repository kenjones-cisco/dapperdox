package spec

import (
	"testing"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
)

const testSpecDir = "../fixtures/"

func TestLoadSpecifications(t *testing.T) {
	config.Restore()
	viper.Set(config.SpecDir, testSpecDir)

	tests := []struct {
		name    string
		specLoc string
		wantErr bool
	}{
		{
			name:    "success - load common specifications",
			specLoc: "common_api.json",
			wantErr: false,
		},
		{
			name:    "success - load specifications with use of allof",
			specLoc: "allof_api.json",
			wantErr: false,
		},
		{
			name:    "success - load specifications with use of path depth",
			specLoc: "depth_api.json",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Set(config.SpecFilename, tt.specLoc)

			if err := LoadSpecifications(nil); (err != nil) != tt.wantErr {
				t.Errorf("LoadSpecifications() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadSpecifications_AutoDiscovery(t *testing.T) {
	config.Restore()
	viper.Set(config.DiscoveryEnabled, true)

	tests := []struct {
		name       string
		specsCache map[string][]byte
		wantErr    bool
	}{
		{
			name:       "success - empty discovery cache",
			specsCache: make(map[string][]byte),
			wantErr:    false,
		},
		{
			name: "success - common api in discovery cache",
			specsCache: map[string][]byte{
				"/path/specs/common": specToByteSlice("../fixtures/common_api.json"),
			},
			wantErr: false,
		},
		{
			name: "success - api with use of allof in discovery cache",
			specsCache: map[string][]byte{
				"/path/specs/allof": specToByteSlice("../fixtures/allof_api.json"),
			},
			wantErr: false,
		},
		{
			name: "success - api with use of path depth in discovery cache",
			specsCache: map[string][]byte{
				"/path/specs/path/depth": specToByteSlice("../fixtures/depth_api.json"),
			},
			wantErr: false,
		},
		{
			name: "success - multiple specs in discovery cache",
			specsCache: map[string][]byte{
				"/path/specs/common":     specToByteSlice("../fixtures/common_api.json"),
				"/path/specs/allof":      specToByteSlice("../fixtures/allof_api.json"),
				"/path/specs/path/depth": specToByteSlice("../fixtures/depth_api.json"),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeDiscoverer{
				specs: tt.specsCache,
			}

			if err := LoadSpecifications(d); (err != nil) != tt.wantErr {
				t.Errorf("LoadSpecifications() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
