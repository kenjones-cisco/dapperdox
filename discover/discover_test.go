package discover

import (
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/kenjones-cisco/dapperdox/discover/models"
)

func TestNewDiscoverer(t *testing.T) {
	d := newDiscovererWithClient(fake.NewSimpleClientset())
	if d == nil {
		t.Fatal("newDiscovererWithClient() returned nil manager")
	}
}

func TestDiscoverer_run_fake_service(_ *testing.T) {
	d := &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}
	go d.Run()

	var once sync.Once

	once.Do(func() {
		time.Sleep(100 * time.Millisecond)
		d.Shutdown()
	})
}

func TestDiscoverer_updateServices(t *testing.T) {
	// handle the initial run and run where data does not change
	d := &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}

	type args struct {
		s *models.Service
		e models.Event
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "initial run",
			args: args{s: testServices[0], e: models.EventAdd},
		},
		{
			name: "next run with no data change",
			args: args{s: testServices[0], e: models.EventUpdate},
		},
		{
			name: "next run with delete data",
			args: args{s: testServices[0], e: models.EventDelete},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			d.updateServices(tt.args.s, tt.args.e)
		})
	}

	// trigger failure within discover()
	d = &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}
	d.updateServices(testServices[0], models.EventAdd)

	// trigger failure from services call
	d = &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}
	d.updateServices(testServices[0], models.EventAdd)

	// trigger no data from services call
	d = &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}
	d.updateServices(testServices[0], models.EventAdd)
}

func TestDiscoverer_updateDeployments(t *testing.T) {
	// handle the initial run and run where data does not change
	d := &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}

	type args struct {
		dpl *models.Deployment
		e   models.Event
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "initial run",
			args: args{dpl: testDeployments[0], e: models.EventAdd},
		},
		{
			name: "next run with no data change",
			args: args{dpl: testDeployments[0], e: models.EventUpdate},
		},
		{
			name: "next run with delete data",
			args: args{dpl: testDeployments[0], e: models.EventDelete},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			d.updateDeployments(tt.args.dpl, tt.args.e)
		})
	}

	// trigger failure within discover()
	d = &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}
	d.updateDeployments(testDeployments[0], models.EventAdd)

	// trigger failure from services call
	d = &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}
	d.updateDeployments(testDeployments[0], models.EventAdd)

	// trigger no data from services call
	d = &Discoverer{data: &state{services: emptyServiceMap}, services: &fakeController{}, stop: make(chan struct{})}
	d.updateDeployments(testDeployments[0], models.EventAdd)
}
