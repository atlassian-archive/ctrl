package ctrl

import (
	"github.com/atlassian/ctrl/logz"
	"go.uber.org/zap"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

type CreatorIndex interface {
	// CreatorByObject returns controller objects that own or want to own an object with a particular Group, Kind,
	// namespace and name. "want to own" means that the object might not exist yet but the controller
	// object would want it to.
	CreatorByObject(gk schema.GroupKind, namespace, name string) ([]runtime.Object, error)
}

// ControlledResourceHandler is a handler for objects the are produced by some parent object.
// The parent object is identified by a controller owner reference on the produced objects.
type ControlledResourceHandler struct {
	Logger        *zap.Logger
	WorkQueue     WorkQueueProducer
	ZapNameField  ZapNameField
	CreatorIndex  CreatorIndex
	ControllerGvk schema.GroupVersionKind
}

func (g *ControlledResourceHandler) OnAdd(obj interface{}) {
	name, namespace := g.getControllerNameAndNamespace(obj)
	logger := g.loggerForObj(obj)
	g.rebuildByName(logger, namespace, name, "added")
}

func (g *ControlledResourceHandler) OnUpdate(oldObj, newObj interface{}) {
	oldName, oldNamespace := g.getControllerNameAndNamespace(oldObj)

	newName, newNamespace := g.getControllerNameAndNamespace(newObj)

	if oldName != newName { // changed controller of the object
		logger := g.loggerForObj(oldObj)
		g.rebuildByName(logger, oldNamespace, oldName, "updated")
	}
	logger := g.loggerForObj(newObj)
	g.rebuildByName(logger, newNamespace, newName, "updated")
}

func (g *ControlledResourceHandler) OnDelete(obj interface{}) {
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
		logger = g.loggerForObj(metaObj)
	}
	name, namespace := g.getControllerNameAndNamespace(metaObj)
	if name == "" { // No controller object found
		if g.CreatorIndex != nil {
			creators, err := g.CreatorIndex.CreatorByObject(
				metaObj.(runtime.Object).GetObjectKind().GroupVersionKind().GroupKind(), namespace, metaObj.GetName())
			if err != nil {
				logger.Error("Failed to get creators for object", zap.Error(err))
				return
			}
			for _, creator := range creators {
				g.rebuildByName(logger, namespace, creator.(meta_v1.Object).GetName(), "deleted")
			}
		}
	} else {
		g.rebuildByName(logger, namespace, name, "deleted")
	}
}

// This method may be called with an empty name.
func (g *ControlledResourceHandler) rebuildByName(logger *zap.Logger, namespace, name, addUpdateDelete string) {
	if name == "" {
		return
	}
	logger.
		With(g.ZapNameField(name)).
		Sugar().Infof("Rebuilding creator object %q because created object was %s", name, addUpdateDelete)
	g.WorkQueue.Add(QueueKey{
		Namespace: namespace,
		Name:      name,
	})
}

// getControllerNameAndNamespace returns name and namespace of the object's controller.
// Returned name may be empty if the object does not have a controller owner reference.
func (g *ControlledResourceHandler) getControllerNameAndNamespace(obj interface{}) (string, string) {
	var name string
	meta := obj.(meta_v1.Object)
	ref := meta_v1.GetControllerOf(meta)
	if ref != nil && ref.APIVersion == g.ControllerGvk.GroupVersion().String() && ref.Kind == g.ControllerGvk.Kind {
		name = ref.Name
	}
	return name, meta.GetNamespace()
}

func (g *ControlledResourceHandler) loggerForObj(obj interface{}) *zap.Logger {
	logger := g.Logger
	metaObj, ok := obj.(meta_v1.Object)
	if ok { // This is conditional to deal with tombstones on delete event
		logger = logger.With(logz.Namespace(metaObj), g.ZapNameField(metaObj.GetName()))
	}
	return logger
}
