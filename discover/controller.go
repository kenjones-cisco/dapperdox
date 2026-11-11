package discover

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/kenjones-cisco/dapperdox/discover/models"
)

const catalogWorkerCount = 3

var errUnsynchronized = errors.New("waiting till full synchronization")

// watcher defines an event loop.
//
// The watcher guarantees the following consistency requirement: registry
// view in the watcher is as AT LEAST as fresh as the moment notification
// arrives, but MAY BE more fresh (e.g. "delete" cancels an "add" event).  For
// example, an event for a service creation will see a service registry without
// the service if the event is immediately followed by the service deletion
// event.
//
// Handlers execute on the single worker queue in the order they are appended.
// Handlers receive the notification event and the associated object.  Note
// that all handlers must be appended before starting the watcher.
type watcher interface {
	// AppendServiceHandler notifies about changes to the service catalog.
	AppendServiceHandler(f func(*models.Service, models.Event))

	// AppendDeploymentHandler notifies about change to the deployment catalog.
	AppendDeploymentHandler(f func(*models.Deployment, models.Event))

	// Run until a signal is received
	Run(stop <-chan struct{})
}

// catalogOptions stores the configurable attributes of a cagalog.
type catalogOptions struct {
	// Namespace the controller watches. If set to meta_v1.NamespaceAll (""), controller watches all namespaces
	WatchedNamespace string
	DomainSuffix     string
	ResyncPeriod     time.Duration
}

// catalog is a collection of synchronized resource watchers
// caches are thread-safe.
type catalog struct {
	services    cacheHandler
	deployments cacheHandler
	queue       Queue
}

type cacheHandler struct {
	informer cache.SharedIndexInformer
	handler  *ChainHandler
}

// newClient creates an in cluster connection to Kubernetes API.
func newClient() (kubernetes.Interface, error) {
	return newClientWithConfig(rest.InClusterConfig)
}

func newClientWithConfig(loadConfig func() (*rest.Config, error)) (kubernetes.Interface, error) {
	config, err := loadConfig()
	if err != nil {
		return nil, err
	}

	// creates the clientset
	return kubernetes.NewForConfig(config)
}

// newCatalog creates a new Kubernetes controller.
func newCatalog(client kubernetes.Interface, options catalogOptions) watcher {
	log().Info(fmt.Sprintf("Service controller watching namespace %q", options.WatchedNamespace))

	// Queue requires a time duration for a retry delay after a handler error
	out := &catalog{
		queue: NewQueue(1 * time.Second),
	}

	out.services = out.createInformer(&v1.Service{}, options.ResyncPeriod,
		func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Services(options.WatchedNamespace).List(ctx, opts)
		},
		func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Services(options.WatchedNamespace).Watch(ctx, opts)
		})

	out.deployments = out.createInformer(&appv1.Deployment{}, options.ResyncPeriod,
		func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
			return client.AppsV1().Deployments(options.WatchedNamespace).List(ctx, opts)
		},
		func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			return client.AppsV1().Deployments(options.WatchedNamespace).Watch(ctx, opts)
		})

	return out
}

// AppendServiceHandler implements a service catalog operation.
func (c *catalog) AppendServiceHandler(f func(*models.Service, models.Event)) {
	c.services.handler.Append(func(obj any, event models.Event) error {
		svc, _ := obj.(*v1.Service)

		log().Debug(fmt.Sprintf("(Handler) Service Details: %v", *svc))

		// Do not handle "kube-system" services
		if svc.Namespace == metav1.NamespaceSystem {
			return nil
		}

		log().Debug(fmt.Sprintf("Handle service %s in namespace %s", svc.Name, svc.Namespace))

		if svcConv := convertService(svc); svcConv != nil {
			f(svcConv, event)
		}

		return nil
	})
}

// AppendDeploymentHandler notifies about change to the deployment catalog.
func (c *catalog) AppendDeploymentHandler(f func(*models.Deployment, models.Event)) {
	c.deployments.handler.Append(func(obj any, event models.Event) error {
		dpl, _ := obj.(*appv1.Deployment)

		// Do not handle "kube-system" services
		if dpl.Namespace == metav1.NamespaceSystem {
			return nil
		}

		log().Debug(fmt.Sprintf("(Handler) Deployment Details: %v", dpl.Status))

		// ensure there is at least one replica in ready state
		// in order to serve the API request to fetch API specs
		//
		// readiness works best with multiple instances deployed
		// single instance deployments would still rely on
		if dpl.Status.ReadyReplicas == 0 {
			log().Debug("no ready replicas, don't trigger event")

			return nil
		}

		log().Debug(fmt.Sprintf("Handle deployment %s in namespace %s", dpl.Name, dpl.Namespace))

		if dplConv := convertDeployment(dpl); dplConv != nil {
			f(dplConv, event)
		}

		return nil
	})
}

// Run all controllers until a signal is received.
func (c *catalog) Run(stop <-chan struct{}) {
	var workers sync.WaitGroup

	workers.Add(catalogWorkerCount)

	go func() {
		defer workers.Done()

		c.queue.Run(stop)
	}()
	go func() {
		defer workers.Done()

		c.services.informer.Run(stop)
	}()
	go func() {
		defer workers.Done()

		c.deployments.informer.Run(stop)
	}()

	<-stop
	workers.Wait()
	log().Info("watcher terminated")
}

// HasSynced reports whether both the service and deployment inventories have
// completed their initial list operations.
func (c *catalog) HasSynced() bool {
	if !c.services.informer.HasSynced() || !c.deployments.informer.HasSynced() {
		return false
	}

	if queue, ok := c.queue.(interface{ Idle() bool }); ok {
		return queue.Idle()
	}

	return true
}

// notify is the first handler in the handler chain.
// Returning an error causes repeated execution of the entire chain.
func (c *catalog) notify(obj any, event models.Event) error {
	if !c.services.informer.HasSynced() {
		return errUnsynchronized
	}

	k, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		log().Error(fmt.Sprintf("Error retrieving key: %v", err))
	} else {
		log().Debug(fmt.Sprintf("Event %s: key %#v", event, k))
	}

	return nil
}

func (c *catalog) createInformer(o runtime.Object, resyncPeriod time.Duration, lf cache.ListWithContextFunc, wf cache.WatchFuncWithContext) cacheHandler {
	handler := &ChainHandler{funcs: []Handler{c.notify}}

	// TODO: finer-grained index (perf)
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{ListWithContextFunc: lf, WatchFuncWithContext: wf}, o,
		resyncPeriod, cache.Indexers{})

	if _, err := informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj any) {
				c.queue.Push(Task{handler: handler.Apply, obj: obj, event: models.EventAdd})
			},
			UpdateFunc: func(old, cur any) {
				if !reflect.DeepEqual(old, cur) {
					c.queue.Push(Task{handler: handler.Apply, obj: cur, event: models.EventUpdate})
				}
			},
			DeleteFunc: func(obj any) {
				c.queue.Push(Task{handler: handler.Apply, obj: obj, event: models.EventDelete})
			},
		}); err != nil {
		// Registration can only fail after an informer has stopped; this one is new.
		panic(fmt.Errorf("register informer handlers: %w", err))
	}

	return cacheHandler{informer: informer, handler: handler}
}
