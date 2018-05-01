package ctrl

import (
	"github.com/atlassian/ctrl/logz"
	"go.uber.org/zap"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type GenericHandler struct {
	Logger       *zap.Logger
	WorkQueue    WorkQueueProducer
	ZapNameField ZapNameField
}

func (g *GenericHandler) OnAdd(obj interface{}) {
	logger := g.loggerForObj(obj)
	metaObj := obj.(meta_v1.Object)
	logger.Info("Enqueuing object because it was added")
	g.add(metaObj)
}

func (g *GenericHandler) OnUpdate(oldObj, newObj interface{}) {
	logger := g.loggerForObj(newObj)
	metaObj := newObj.(meta_v1.Object)
	logger.Info("Enqueuing object because it was updated")
	g.add(metaObj)
}

func (g *GenericHandler) OnDelete(obj interface{}) {
	logger := g.loggerForObj(obj)
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
	g.add(metaObj)
}

func (g *GenericHandler) add(obj meta_v1.Object) {
	g.WorkQueue.Add(QueueKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	})
}

func (g *GenericHandler) loggerForObj(obj interface{}) *zap.Logger {
	logger := g.Logger
	metaObj, ok := obj.(meta_v1.Object)
	if ok { // This is conditional to deal with tombstones on delete event
		logger = logger.With(logz.Namespace(metaObj), g.ZapNameField(metaObj.GetName()))
	}
	return logger
}
