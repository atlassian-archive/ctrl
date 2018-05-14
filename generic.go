package ctrl

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ash2k/stager"
	"github.com/atlassian/ctrl/logz"
	chimw "github.com/go-chi/chi/middleware"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	// Work queue deduplicates scheduled keys. This is the period it waits for duplicate keys before letting the work
	// to be dequeued.
	workDeduplicationPeriod = 50 * time.Millisecond
)

type Generic struct {
	iter        uint32
	logger      *zap.Logger
	queue       workQueue
	workers     int
	Controllers map[schema.GroupVersionKind]Holder
	Servers     map[schema.GroupVersionKind]ServerHolder
	Informers   map[schema.GroupVersionKind]cache.SharedIndexInformer
}

func NewGeneric(config *Config, queue workqueue.RateLimitingInterface, workers int, constructors ...Constructor) (*Generic, error) {
	controllers := make(map[schema.GroupVersionKind]Interface)
	servers := make(map[schema.GroupVersionKind]Server)
	holders := make(map[schema.GroupVersionKind]Holder)
	informers := make(map[schema.GroupVersionKind]cache.SharedIndexInformer)
	serverHolders := make(map[schema.GroupVersionKind]ServerHolder)
	wq := workQueue{
		queue: queue,
		workDeduplicationPeriod: workDeduplicationPeriod,
	}
	replacer := strings.NewReplacer(".", "_", "-", "_", "/", "_")
	for _, constr := range constructors {
		descr := constr.Describe()

		readyForWork := make(chan struct{})
		queueGvk := wq.newQueueForGvk(descr.Gvk)
		gvkLogger := config.Logger.With(logz.Gvk(descr.Gvk))
		constructorConfig := config
		constructorConfig.Logger = gvkLogger

		// Extra controller data
		groupKind := descr.Gvk.GroupKind()
		objectName := replacer.Replace(groupKind.String())

		// Extra api data
		requestCount := prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("request_%s_count", objectName),
				Help: fmt.Sprintf("Cumulative number of %s processed", &groupKind),
			},
			[]string{"url", "method", "status"},
		)
		requestTime := prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name: fmt.Sprintf("request_%s_time", objectName),
				Help: fmt.Sprintf("Number of seconds each request to %s takes", &groupKind),
			},
		)

		var allMetrics []prometheus.Collector

		constructed, err := constr.New(
			constructorConfig,
			&Context{
				ReadyForWork: func() {
					close(readyForWork)
				},
				Middleware:  addMiddleware(requestCount, requestTime),
				Informers:   informers,
				Controllers: controllers,
				WorkQueue:   queueGvk,
			},
		)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to construct controller for GVK %s", descr.Gvk)
		}

		if constructed.Interface == nil && constructed.Server == nil {
			return nil, errors.Wrapf(err, "failed to construct controller or server for GVK %s", descr.Gvk)
		}

		if constructed.Interface != nil {
			if _, ok := controllers[descr.Gvk]; ok {
				return nil, errors.Errorf("duplicate controller for GVK %s", descr.Gvk)
			}
			inf, ok := informers[descr.Gvk]
			if !ok {
				return nil, errors.Errorf("controller for GVK %s should have registered an informer for that GVK", descr.Gvk)
			}
			inf.AddEventHandler(&GenericHandler{
				Logger:       gvkLogger,
				WorkQueue:    queueGvk,
				ZapNameField: descr.ZapNameField,
			})

			controllers[descr.Gvk] = constructed.Interface

			objectProcessTime := prometheus.NewHistogram(
				prometheus.HistogramOpts{
					Namespace: constructorConfig.AppName,
					Name:      fmt.Sprintf("process_%s", objectName),
					Help:      fmt.Sprintf("Histogram measuring the time it took to process a %s", &groupKind),
				},
			)

			holders[descr.Gvk] = Holder{
				Cntrlr:            constructed.Interface,
				ZapNameField:      descr.ZapNameField,
				ReadyForWork:      readyForWork,
				objectProcessTime: objectProcessTime,
			}

			allMetrics = append(allMetrics, objectProcessTime)
		}

		if constructed.Server != nil {
			if _, ok := servers[descr.Gvk]; ok {
				return nil, errors.Errorf("duplicate server for GVK %s", descr.Gvk)
			}
			servers[descr.Gvk] = constructed.Server

			serverHolders[descr.Gvk] = ServerHolder{
				Server:       constructed.Server,
				ZapNameField: descr.ZapNameField,
				requestCount: requestCount,
				requestTime:  requestTime,
			}

			allMetrics = append(allMetrics, requestCount, requestTime)
		}

		for _, metric := range allMetrics {
			if err := constructorConfig.Registry.Register(metric); err != nil {
				return nil, errors.WithStack(err)
			}
		}
	}

	return &Generic{
		logger:      config.Logger,
		queue:       wq,
		workers:     workers,
		Controllers: holders,
		Servers:     serverHolders,
		Informers:   informers,
	}, nil
}

func (g *Generic) Run(ctx context.Context) error {
	// Stager will perform ordered, graceful shutdown
	stgr := stager.New()
	defer stgr.Shutdown()
	defer g.queue.shutDown()

	// Stage: start all informers then wait on them
	stage := stgr.NextStage()
	for _, inf := range g.Informers {
		stage.StartWithChannel(inf.Run)
	}
	g.logger.Info("Waiting for informers to sync")
	for _, inf := range g.Informers {
		if !cache.WaitForCacheSync(ctx.Done(), inf.HasSynced) {
			return ctx.Err()
		}
	}
	g.logger.Info("Informers synced")

	// Stage: start all controllers then wait for them to signal ready for work
	stage = stgr.NextStage()
	for _, c := range g.Controllers {
		stage.StartWithContext(c.Cntrlr.Run)
	}
	for gvk, c := range g.Controllers {
		select {
		case <-ctx.Done():
			g.logger.Sugar().Infof("Was waiting for the controller for %s to become ready for processing", gvk)
			return ctx.Err()
		case <-c.ReadyForWork:
		}
	}

	// Stage: start workers
	stage = stgr.NextStage()
	for i := 0; i < g.workers; i++ {
		stage.Start(g.worker)
	}

	if len(g.Servers) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	// Stage: start servers
	group, ctx := errgroup.WithContext(ctx)
	for _, srv := range g.Servers {
		server := srv.Server // capture field into a scoped variable to avoid data race
		group.Go(func() error {
			return server.Run(ctx)
		})
	}
	return group.Wait()
}

func addMiddleware(requestCount *prometheus.CounterVec, requestTime prometheus.Histogram) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			t0 := time.Now()
			next.ServeHTTP(res, r)
			tn := time.Since(t0)

			requestCount.WithLabelValues(r.URL.Path, r.Method, string(res.Status())).Inc()
			requestTime.Observe(tn.Seconds())
		})
	}
}

type Holder struct {
	Cntrlr            Interface
	ZapNameField      ZapNameField
	ReadyForWork      <-chan struct{}
	objectProcessTime prometheus.Histogram
}

type ServerHolder struct {
	Server       Server
	ZapNameField ZapNameField
	ReadyForWork <-chan struct{}
	requestCount *prometheus.CounterVec
	requestTime  prometheus.Histogram
}
