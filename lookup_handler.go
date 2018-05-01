package ctrl

import (
	"github.com/atlassian/ctrl/logz"
	"go.uber.org/zap"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// LookupHandler is a handler for objects that can be mapped to some parent object
// through the use of an SharedIndexInformer.
type LookupHandler struct {
	Logger       *zap.Logger
	WorkQueue    WorkQueueProducer
	ZapNameField ZapNameField

	Lookup func(runtime.Object) ([]runtime.Object, error)
}

func (e *LookupHandler) enqueueMapped(logger *zap.Logger, obj meta_v1.Object) {
	objs, err := e.Lookup(obj.(runtime.Object))
	if err != nil {
		logger.Error("Failed to get objects from index", zap.Error(err))
		return
	}
	for _, o := range objs {
		metaobj := o.(meta_v1.Object)
		e.WorkQueue.Add(QueueKey{
			Namespace: metaobj.GetNamespace(),
			Name:      metaobj.GetName(),
		})
	}
}

func (e *LookupHandler) OnAdd(obj interface{}) {
	logger := e.loggerForObj(obj)
	metaObj := obj.(meta_v1.Object)
	logger.Info("Enqueuing mapped objects because it was added")
	e.enqueueMapped(logger, metaObj)
}

func (e *LookupHandler) OnUpdate(oldObj, newObj interface{}) {
	logger := e.loggerForObj(newObj)
	metaObj := newObj.(meta_v1.Object)
	logger.Info("Enqueuing mapped objects because it was updated")
	e.enqueueMapped(logger, metaObj)
}

func (e *LookupHandler) OnDelete(obj interface{}) {
	logger := e.loggerForObj(obj)
	metaObj, ok := obj.(meta_v1.Object)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			logger.Sugar().Errorf("Delete event with unrecognized object type: %T", obj)
			return
		}
		metaObj, ok = tombstone.Obj.(meta_v1.Object)
		if !ok {
			logger.Sugar().Errorf("Delete tombstone with unrecognized object type: %T", tombstone.Obj)
			return
		}
	}
	e.enqueueMapped(logger, metaObj)
}

func (e *LookupHandler) loggerForObj(obj interface{}) *zap.Logger {
	logger := e.Logger
	metaObj, ok := obj.(meta_v1.Object)
	if ok { // This is conditional to deal with tombstones on delete event
		logger = logger.With(logz.Namespace(metaObj), e.ZapNameField(metaObj.GetName()))
	}
	return logger
}
