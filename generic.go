package ctrl

import (
	"context"
	"time"

	"github.com/ash2k/stager"
	"github.com/pkg/errors"
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
	logger      *zap.Logger
	queue       workQueue
	workers     int
	Controllers map[schema.GroupVersionKind]Holder
	Informers   map[schema.GroupVersionKind]cache.SharedIndexInformer
}

func NewGeneric(config *Config, logger *zap.Logger, queue workqueue.RateLimitingInterface, workers int, constructors ...Constructor) (*Generic, error) {
	controllers := make(map[schema.GroupVersionKind]Interface, len(constructors))
	holders := make(map[schema.GroupVersionKind]Holder, len(constructors))
	informers := make(map[schema.GroupVersionKind]cache.SharedIndexInformer)
	wq := workQueue{
		queue: queue,
		workDeduplicationPeriod: workDeduplicationPeriod,
	}
	for _, constr := range constructors {
		descr := constr.Describe()
		if _, ok := controllers[descr.Gvk]; ok {
			return nil, errors.Errorf("duplicate controller for GVK %s", descr.Gvk)
		}
		readyForWork := make(chan struct{})
		queueGvk := wq.newQueueForGvk(descr.Gvk)
		iface, err := constr.New(config, &Context{
			ReadyForWork: func() {
				close(readyForWork)
			},
			Informers:   informers,
			Controllers: controllers,
			WorkQueue:   queueGvk,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to construct controller for GVK %s", descr.Gvk)
		}
		inf, ok := informers[descr.Gvk]
		if !ok {
			return nil, errors.Errorf("controller for GVK %s should have registered an informer for that GVK", descr.Gvk)
		}
		inf.AddEventHandler(&GenericHandler{
			Logger:       logger,
			Queue:        queueGvk,
			ZapNameField: descr.ZapNameField,
		})
		controllers[descr.Gvk] = iface
		holders[descr.Gvk] = Holder{
			Cntrlr:       iface,
			ZapNameField: descr.ZapNameField,
			ReadyForWork: readyForWork,
		}
	}
	return &Generic{
		logger:      logger,
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
	Cntrlr       Interface
	ZapNameField ZapNameField
	ReadyForWork <-chan struct{}
}
