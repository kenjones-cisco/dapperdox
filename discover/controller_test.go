package discover

import (
	"errors"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/kenjones-cisco/dapperdox/discover/models"
)

const (
	resync = 1 * time.Second
)

func Test_NewClient(t *testing.T) {
	configErr := errors.New("configuration unavailable")
	if _, err := newClientWithConfig(func() (*rest.Config, error) {
		return nil, configErr
	}); !errors.Is(err, configErr) {
		t.Fatalf("newClientWithConfig() error = %v, want %v", err, configErr)
	}

	client, err := newClientWithConfig(func() (*rest.Config, error) {
		return &rest.Config{Host: "https://127.0.0.1"}, nil
	})
	if err != nil {
		t.Fatalf("newClientWithConfig() error = %v", err)
	}

	if client == nil {
		t.Fatal("newClientWithConfig() returned nil client")
	}
}

func setupServices(t *testing.T, catalog *catalog, namespace string) {
	t.Helper()

	createService(t, catalog, "svc1", namespace, nil, []int32{8080}, map[string]string{"app": "prod-app"})
	createService(t, catalog, "svc2", namespace, nil, []int32{8081}, map[string]string{"app": "staging-app"})
}

func TestController_Services(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)

	namespace := "nsA"
	catalog := makeFakeKubeAPIController(namespace)

	go catalog.Run(stop)

	setupServices(t, catalog, namespace)
}

func makeFakeKubeAPIController(namespace string) *catalog {
	if namespace == "" {
		namespace = defaultVal
	}

	clientSet := fake.NewSimpleClientset()
	ctlg := newCatalog(clientSet, catalogOptions{
		WatchedNamespace: namespace,
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
	})
	catalog, _ := ctlg.(*catalog)

	catalog.AppendServiceHandler(updateServicesHandler)
	catalog.AppendDeploymentHandler(updateDeploymentHandler)

	return catalog
}

func updateServicesHandler(*models.Service, models.Event)      {}
func updateDeploymentHandler(*models.Deployment, models.Event) {}

func createService(t *testing.T, catalog *catalog, name, namespace string, annotations map[string]string, ports []int32, selector map[string]string) {
	t.Helper()

	svcPorts := []v1.ServicePort{}

	for _, p := range ports {
		svcPorts = append(svcPorts, v1.ServicePort{
			Name:     "test-port",
			Port:     p,
			Protocol: "http",
		})
	}

	service := &v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports:     svcPorts,
			Selector:  selector,
			Type:      v1.ServiceTypeClusterIP,
		},
	}

	if err := catalog.services.informer.GetStore().Add(service); err != nil {
		t.Errorf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}
