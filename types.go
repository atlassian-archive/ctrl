package ctrl

import (
	"context"
	"flag"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// ZapNameField is a function that can be used to obtain structured logging field for an object's name.
type ZapNameField func(name string) zap.Field

type Descriptor struct {
	// Group Version Kind of objects a controller can process.
	Gvk          schema.GroupVersionKind
	ZapNameField ZapNameField
}

type Constructor interface {
	AddFlags(*flag.FlagSet)
	// New constructs a new controller.
	// It must register an informer for the GVK controller handles via Context.RegisterInformer().
	New(*Config, *Context) (Interface, error)
	Describe() Descriptor
}

type Interface interface {
	Run(context.Context)
	Process(*ProcessContext) (retriable bool, err error)
}

type WorkQueueProducer interface {
	// Add adds an item to the workqueue.
	Add(QueueKey)
}

type ProcessContext struct {
	Logger *zap.Logger
	Object runtime.Object
}

type QueueKey struct {
	Namespace string
	Name      string
}

type Config struct {
	AppName      string
	Logger       *zap.Logger
	Namespace    string
	ResyncPeriod time.Duration
	Registry     prometheus.Registerer

	RestConfig *rest.Config
	MainClient kubernetes.Interface
}

type Context struct {
	// ReadyForWork is a function that the controller must call from its Run() method once it is ready to
	// process work using it's Process() method. This should be used to delay processing while some initialization
	// is being performed.
	ReadyForWork func()
	// Will contain all informers once Generic controller constructs all controllers.
	// This is a read only field, must not be modified.
	Informers map[schema.GroupVersionKind]cache.SharedIndexInformer
	// Will contain all controllers once Generic controller constructs them.
	// This is a read only field, must not be modified.
	Controllers map[schema.GroupVersionKind]Interface
	WorkQueue   WorkQueueProducer
}

func (c *Context) RegisterInformer(gvk schema.GroupVersionKind, inf cache.SharedIndexInformer) error {
	if _, ok := c.Informers[gvk]; ok {
		return errors.New("informer with this GVK has been registered already")
	}
	if c.Informers == nil {
		c.Informers = make(map[schema.GroupVersionKind]cache.SharedIndexInformer)
	}
	c.Informers[gvk] = inf
	return nil
}

func (c *Context) MainInformer(config *Config, gvk schema.GroupVersionKind, f func(kubernetes.Interface, string, time.Duration, cache.Indexers) cache.SharedIndexInformer) (cache.SharedIndexInformer, error) {
	inf := c.Informers[gvk]
	if inf == nil {
		inf = f(config.MainClient, config.Namespace, config.ResyncPeriod, cache.Indexers{})
		err := c.RegisterInformer(gvk, inf)
		if err != nil {
			return nil, err
		}
	}
	return inf, nil
}

func (c *Context) MainClusterInformer(config *Config, gvk schema.GroupVersionKind, f func(kubernetes.Interface, time.Duration, cache.Indexers) cache.SharedIndexInformer) (cache.SharedIndexInformer, error) {
	inf := c.Informers[gvk]
	if inf == nil {
		inf = f(config.MainClient, config.ResyncPeriod, cache.Indexers{})
		err := c.RegisterInformer(gvk, inf)
		if err != nil {
			return nil, err
		}
	}
	return inf, nil
}
