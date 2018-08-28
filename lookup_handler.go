package ctrl

import (
	"github.com/atlassian/ctrl/logz"
	"go.uber.org/zap"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// LookupHandler is a handler for controlled objects that can be mapped to some controller object
// through the use of a Lookup function.
// This handler assumes that the Logger already has the ctrl_gk field set.
type LookupHandler struct {
	Logger    *zap.Logger
	WorkQueue WorkQueueProducer
	Gvk       schema.GroupVersionKind

	Lookup func(runtime.Object) ([]runtime.Object, error)
}

func (e *LookupHandler) enqueueMapped(obj meta_v1.Object, addUpdateDelete string) {
	logger := e.loggerForObj(obj)
	objs, err := e.Lookup(obj.(runtime.Object))
	if err != nil {
		logger.Error("Failed to lookup objects", zap.Error(err))
		return
	}
	for _, o := range objs {
		metaobj := o.(meta_v1.Object)
		logger.
			With(logz.Delegate(metaobj)).
			With(logz.DelegateGk(o.GetObjectKind().GroupVersionKind().GroupKind())).
			Sugar().Infof("Enqueuing looked up object '%s' because parent object was %s", obj.GetNamespace(), obj.GetName(), addUpdateDelete)
		e.WorkQueue.Add(QueueKey{
			Namespace: metaobj.GetNamespace(),
			Name:      metaobj.GetName(),
		})
	}
}

func (e *LookupHandler) OnAdd(obj interface{}) {
	e.enqueueMapped(obj.(meta_v1.Object), "added")
}

func (e *LookupHandler) OnUpdate(oldObj, newObj interface{}) {
	e.enqueueMapped(newObj.(meta_v1.Object), "updated")
}

func (e *LookupHandler) OnDelete(obj interface{}) {
	metaObj, ok := obj.(meta_v1.Object)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			e.Logger.Sugar().Errorf("Delete event with unrecognized object type: %T", obj)
			return
		}
		metaObj, ok = tombstone.Obj.(meta_v1.Object)
		if !ok {
			e.Logger.Sugar().Errorf("Delete tombstone with unrecognized object type: %T", tombstone.Obj)
			return
		}
	}
	e.enqueueMapped(metaObj, "deleted")
}

// loggerForObj returns a logger with fields for a controlled object.
func (e *LookupHandler) loggerForObj(obj meta_v1.Object) *zap.Logger {
	return e.Logger.With(logz.Namespace(obj),
		logz.Object(obj),
		logz.ObjectGk(e.Gvk.GroupKind()))
}
