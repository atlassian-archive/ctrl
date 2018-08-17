package ctrl

import (
	"github.com/atlassian/ctrl/logz"
	"go.uber.org/zap"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// ControllerIndex is an index from controlled to controller objects.
type ControllerIndex interface {
	// ControllerByObject returns controller objects that own or want to own an object with a particular Group, Kind,
	// namespace and name. "want to own" means that the object might not exist yet but the controller
	// object would want it to.
	ControllerByObject(gk schema.GroupKind, namespace, name string) ([]runtime.Object, error)
}

// ControlledResourceHandler is a handler for objects the are controlled/owned/produced by some controller object.
// The controller object is identified by a controller owner reference on the controlled objects.
// This handler assumes that:
// - Logger already has the cntrl_gk field set.
// - controlled and controller objects exist in the same namespace and never across namespaces.
type ControlledResourceHandler struct {
	Logger          *zap.Logger
	WorkQueue       WorkQueueProducer
	ControllerIndex ControllerIndex
	ControllerGvk   schema.GroupVersionKind
}

func (g *ControlledResourceHandler) enqueueMapped(metaObj meta_v1.Object, action string) {
	name, namespace := g.getControllerNameAndNamespace(metaObj)
	logger := g.loggerForObj(metaObj)

	if name == "" {
		if g.ControllerIndex != nil {
			controllers, err := g.ControllerIndex.ControllerByObject(
				metaObj.(runtime.Object).GetObjectKind().GroupVersionKind().GroupKind(), namespace, metaObj.GetName())
			if err != nil {
				logger.Error("Failed to get controllers for object", zap.Error(err))
				return
			}
			for _, controller := range controllers {
				controllerMeta := controller.(meta_v1.Object)
				g.rebuildControllerByName(logger, controllerMeta.GetNamespace(), controllerMeta.GetName(), action)
			}
		}
	} else {
		g.rebuildControllerByName(logger, namespace, name, action)
	}
}

func (g *ControlledResourceHandler) OnAdd(obj interface{}) {
	metaObj := obj.(meta_v1.Object)
	g.enqueueMapped(metaObj, "added")
}

func (g *ControlledResourceHandler) OnUpdate(oldObj, newObj interface{}) {
	oldMeta := oldObj.(meta_v1.Object)
	newMeta := newObj.(meta_v1.Object)

	oldName, _ := g.getControllerNameAndNamespace(oldMeta)
	newName, _ := g.getControllerNameAndNamespace(newMeta)

	if oldName != newName {
		g.enqueueMapped(oldMeta, "updated")
	}

	g.enqueueMapped(newMeta, "updated")
}

func (g *ControlledResourceHandler) OnDelete(obj interface{}) {
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
	g.enqueueMapped(metaObj, "deleted")
}

// This method may be called with an empty controllerName.
func (g *ControlledResourceHandler) rebuildControllerByName(logger *zap.Logger, namespace, controllerName, addUpdateDelete string) {
	if controllerName == "" {
		return
	}
	logger.
		With(logz.ControllerName(controllerName)).
		Sugar().Infof("Enqueuing controller object because controlled object was %s", addUpdateDelete)
	g.WorkQueue.Add(QueueKey{
		Namespace: namespace,
		Name:      controllerName,
	})
}

// getControllerNameAndNamespace returns name and namespace of the object's controller.
// Returned name may be empty if the object does not have a controller owner reference.
func (g *ControlledResourceHandler) getControllerNameAndNamespace(obj meta_v1.Object) (string, string) {
	var name string
	ref := meta_v1.GetControllerOf(obj)
	if ref != nil && ref.APIVersion == g.ControllerGvk.GroupVersion().String() && ref.Kind == g.ControllerGvk.Kind {
		name = ref.Name
	}
	return name, obj.GetNamespace()
}

// loggerForObj returns a logger with fields for a controlled object.
func (g *ControlledResourceHandler) loggerForObj(obj meta_v1.Object) *zap.Logger {
	return g.Logger.With(logz.Namespace(obj), logz.Object(obj),
		logz.ObjectGk(obj.(runtime.Object).GetObjectKind().GroupVersionKind().GroupKind()))
}
