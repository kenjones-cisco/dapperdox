package handlers

import (
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/discover"
)

func TestUpdater_AutoDiscoverUpdater(t *testing.T) {
	config.Restore()

	viper.Set(config.DiscoveryEnabled, true)
	viper.Set(config.DiscoveryPeriodTime, "1s")

	fd := &fakeDiscover{
		t: t,
	}

	updater := NewAutoDiscoverUpdater(fd)
	defer updater.Close()

	type fields struct {
		specPathMap map[string]string
	}

	tests := []struct {
		name      string
		fields    fields
		wantSpecs map[string][]byte
		wantErr   bool
	}{
		{
			name: "success - empty specs to be loaded by auto-discovery",
			fields: fields{
				specPathMap: make(map[string]string),
			},
			wantSpecs: make(map[string][]byte),
			wantErr:   false,
		},
		{
			name: "success - common specs to be loaded by auto-discovery",
			fields: fields{
				specPathMap: map[string]string{
					"/path/specs/common": "../fixtures/common_api.json",
				},
			},
			wantSpecs: map[string][]byte{
				"/path/specs/common": specToByteSlice("../fixtures/common_api.json"),
			},
		},
		{
			name: "success - allof specs to be loaded by auto-discovery",
			fields: fields{
				specPathMap: map[string]string{
					"/path/specs/allof": "../fixtures/allof_api.json",
				},
			},
			wantSpecs: map[string][]byte{
				"/path/specs/allof": specToByteSlice("../fixtures/allof_api.json"),
			},
			wantErr: false,
		},
		{
			name: "success - depth specs to be loaded by auto-discovery",
			fields: fields{
				specPathMap: map[string]string{
					"/path/specs/depth": "../fixtures/depth_api.json",
				},
			},
			wantSpecs: map[string][]byte{
				"/path/specs/depth": specToByteSlice("../fixtures/depth_api.json"),
			},
			wantErr: false,
		},
		{
			name: "success - multiple specs to be loaded by auto-discovery",
			fields: fields{
				specPathMap: map[string]string{
					"/path/specs/common": "../fixtures/common_api.json",
					"/path/specs/allof":  "../fixtures/allof_api.json",
					"/path/specs/depth":  "../fixtures/depth_api.json",
				},
			},
			wantSpecs: map[string][]byte{
				"/path/specs/common": specToByteSlice("../fixtures/common_api.json"),
				"/path/specs/allof":  specToByteSlice("../fixtures/allof_api.json"),
				"/path/specs/depth":  specToByteSlice("../fixtures/depth_api.json"),
			},
			wantErr: false,
		},
		{
			name: "fail - incorrect path comparison",
			fields: fields{
				specPathMap: map[string]string{
					"/wrong/path/common": "../fixtures/common_api.json",
				},
			},
			wantSpecs: map[string][]byte{
				"/path/specs/common": specToByteSlice("../fixtures/common_api.json"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fd.sPaths = tt.fields.specPathMap
			fd.wantSpecs = tt.wantSpecs
			fd.wantErr = tt.wantErr

			time.Sleep(time.Second * 2)
		})
	}
}

func TestUpdater_Close(t *testing.T) {
	updater := &Updater{
		d:      discover.NewDefaultDiscoverer(),
		r:      mux.NewRouter(),
		ticker: time.NewTicker(time.Second * 1),
		done:   make(chan bool),
	}

	closed := false

	go func(u *Updater) {
		for range u.done {
			closed = true
		}
	}(updater)

	updater.Close()

	if !closed {
		t.Error("updater.Close() failed to clean up")
	}

	updater.Close()
}
