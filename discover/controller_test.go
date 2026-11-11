package discover

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/kenjones-cisco/dapperdox/discover/models"
)

const (
	resync = 1 * time.Second
)

var errTestConfig = errors.New("configuration unavailable")

func Test_NewClient(t *testing.T) {
	configErr := errTestConfig
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

func TestCatalogCancelsAPIRequestsOnShutdown(t *testing.T) {
	for _, blockWatch := range []bool{false, true} {
		name := "list"
		if blockWatch {
			name = "watch"
		}

		t.Run(name, func(t *testing.T) {
			started := make(chan struct{}, 2)
			canceled := make(chan struct{}, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if (r.URL.Query().Get("watch") == "true") == blockWatch {
					if blockWatch {
						w.Header().Set("Content-Type", "application/json")
						w.(http.Flusher).Flush()
					}

					started <- struct{}{}

					<-r.Context().Done()

					canceled <- struct{}{}

					return
				}

				kind, apiVersion := "ServiceList", "v1"
				if strings.HasSuffix(r.URL.Path, "/deployments") {
					kind, apiVersion = "DeploymentList", "apps/v1"
				}

				w.Header().Set("Content-Type", "application/json")

				if err := json.NewEncoder(w).Encode(map[string]any{
					"kind": kind, "apiVersion": apiVersion,
					"metadata": map[string]string{"resourceVersion": "1"}, "items": []any{},
				}); err != nil {
					t.Errorf("encode Kubernetes list response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			client, err := newClientWithConfig(func() (*rest.Config, error) {
				return &rest.Config{Host: server.URL}, nil
			})
			if err != nil {
				t.Fatal(err)
			}

			controller := newCatalog(client, catalogOptions{WatchedNamespace: "docs"})
			stop := make(chan struct{})
			done := make(chan struct{})

			var once sync.Once

			stopController := func() { once.Do(func() { close(stop) }) }

			go func() {
				defer close(done)

				controller.Run(stop)
			}()

			t.Cleanup(stopController)

			for range 2 {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("Kubernetes requests did not start")
				}
			}

			stopController()

			for range 2 {
				select {
				case <-canceled:
				case <-time.After(5 * time.Second):
					t.Fatal("shutdown did not cancel Kubernetes HTTP request")
				}
			}

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("catalog did not stop after canceling API requests")
			}
		})
	}
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

	svcPorts := make([]v1.ServicePort, 0, len(ports))

	for _, p := range ports {
		svcPorts = append(svcPorts, v1.ServicePort{
			Name:     "test-port",
			Port:     p,
			Protocol: "http",
		})
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
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
