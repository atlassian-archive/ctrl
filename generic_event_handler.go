package ctrl

import (
	"github.com/atlassian/ctrl/logz"
	"go.uber.org/zap"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GenericHandler struct {
	Logger       *zap.Logger
	WorkQueue    WorkQueueProducer
	ZapNameField ZapNameField
}

func (g *GenericHandler) OnAdd(obj interface{}) {
	metaObj := obj.(meta_v1.Object)
	g.Logger.Info("Enqueuing object because it was added", logz.Namespace(metaObj), g.ZapNameField(metaObj.GetName()))
	g.add(metaObj)
}

func (g *GenericHandler) OnUpdate(oldObj, newObj interface{}) {
	metaObj := newObj.(meta_v1.Object)
	g.Logger.Info("Enqueuing object because it was updated", logz.Namespace(metaObj), g.ZapNameField(metaObj.GetName()))
	g.add(metaObj)
}

func (g *GenericHandler) OnDelete(obj interface{}) {
	metaObj := obj.(meta_v1.Object)
	g.Logger.Info("Object was deleted", logz.Namespace(metaObj), g.ZapNameField(metaObj.GetName()))
	g.add(metaObj)
}

func (g *GenericHandler) add(obj meta_v1.Object) {
	g.WorkQueue.Add(QueueKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	})
}
