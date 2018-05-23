package ctrl

import (
	"github.com/atlassian/ctrl/logz"
	"go.uber.org/zap"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// This handler assumes that the Logger already has the obj_gk/ctrl_gk field set.
type GenericHandler struct {
	Logger       *zap.Logger
	WorkQueue    WorkQueueProducer
	ZapNameField ZapNameField
}

func (g *GenericHandler) OnAdd(obj interface{}) {
	g.add(obj.(meta_v1.Object), "added")
}

func (g *GenericHandler) OnUpdate(oldObj, newObj interface{}) {
	g.add(newObj.(meta_v1.Object), "updated")
}

func (g *GenericHandler) OnDelete(obj interface{}) {
	metaObj, ok := obj.(meta_v1.Object)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			g.Logger.Sugar().Errorf("Delete event with unrecognized object type: %T", obj)
			return
		}
		metaObj, ok = tombstone.Obj.(meta_v1.Object)
		if !ok {
			g.Logger.Sugar().Errorf("Delete tombstone with unrecognized object type: %T", tombstone.Obj)
			return
		}
	}
	g.add(metaObj, "deleted")
}

func (g *GenericHandler) add(obj meta_v1.Object, addUpdateDelete string) {
	g.loggerForObj(obj).Sugar().Infof("Enqueuing object because it was %s", addUpdateDelete)
	g.WorkQueue.Add(QueueKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	})
}

func (g *GenericHandler) loggerForObj(obj meta_v1.Object) *zap.Logger {
	return g.Logger.With(logz.Namespace(obj), g.ZapNameField(obj.GetName()))
}
