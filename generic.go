package ctrl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ash2k/stager"
	"github.com/atlassian/ctrl/logz"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
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
	Informers   map[schema.GroupVersionKind]cache.SharedIndexInformer
}

func NewGeneric(config *Config, queue workqueue.RateLimitingInterface, workers int, constructors ...Constructor) (*Generic, error) {
	controllers := make(map[schema.GroupVersionKind]Interface, len(constructors))
	holders := make(map[schema.GroupVersionKind]Holder, len(constructors))
	informers := make(map[schema.GroupVersionKind]cache.SharedIndexInformer)
	wq := workQueue{
		queue: queue,
		workDeduplicationPeriod: workDeduplicationPeriod,
	}
	replacer := strings.NewReplacer(".", "_", "-", "_", "/", "_")
	for _, constr := range constructors {
		descr := constr.Describe()
		if _, ok := controllers[descr.Gvk]; ok {
			return nil, errors.Errorf("duplicate controller for GVK %s", descr.Gvk)
		}
		readyForWork := make(chan struct{})
		queueGvk := wq.newQueueForGvk(descr.Gvk)
		gvkLogger := config.Logger.With(logz.Gvk(descr.Gvk))
		constructorConfig := config
		constructorConfig.Logger = gvkLogger

		iface, err := constr.New(
			constructorConfig,
			&Context{
				ReadyForWork: func() {
					close(readyForWork)
				},
				Informers:   informers,
				Controllers: controllers,
				WorkQueue:   queueGvk,
			},
		)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to construct controller for GVK %s", descr.Gvk)
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
		controllers[descr.Gvk] = iface

		// Extra controller data
		groupKind := descr.Gvk.GroupKind()
		objectName := replacer.Replace(groupKind.String())

		objectProcessCount := prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: constructorConfig.AppName,
				Name:      fmt.Sprintf("processed_%s_count", objectName),
				Help:      fmt.Sprintf("Cumulative number of %s processed", &groupKind),
			},
		)

		objectProcessTime := prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: constructorConfig.AppName,
				Name:      fmt.Sprintf("process_%s_seconds", objectName),
				Help:      fmt.Sprintf("Histogram measuring the time it took to process a %s", &groupKind),
			},
		)
		allMetrics := []prometheus.Collector{
			objectProcessCount, objectProcessTime,
		}
		for _, metric := range allMetrics {
			if err := constructorConfig.Registry.Register(metric); err != nil {
				return nil, errors.WithStack(err)
			}
		}

		holders[descr.Gvk] = Holder{
			Cntrlr:             iface,
			ZapNameField:       descr.ZapNameField,
			ReadyForWork:       readyForWork,
			objectProcessCount: objectProcessCount,
			objectProcessTime:  objectProcessTime,
		}
	}

	return &Generic{
		logger:      config.Logger,
		queue:       wq,
		workers:     workers,
		Controllers: holders,
		Informers:   informers,
	}, nil
}

func (g *Generic) Run(ctx context.Context) {
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
			return
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
			return
		case <-c.ReadyForWork:
		}
	}

	// Stage: start workers
	stage = stgr.NextStage()
	for i := 0; i < g.workers; i++ {
		stage.Start(g.worker)
	}

	<-ctx.Done()
}

type Holder struct {
	Cntrlr             Interface
	ZapNameField       ZapNameField
	ReadyForWork       <-chan struct{}
	objectProcessCount prometheus.Counter
	objectProcessTime  prometheus.Histogram
}
