package app

import (
	"time"

	"github.com/atlassian/ctrl"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultResyncPeriod = 20 * time.Minute
)

type GenericControllerOptions struct {
	ResyncPeriod time.Duration
	Workers      int
}

func BindGenericControllerFlags(o *GenericControllerOptions, fs ctrl.FlagSet) {
	fs.DurationVar(&o.ResyncPeriod, "resync-period", defaultResyncPeriod, "Resync period for informers")
	fs.IntVar(&o.Workers, "workers", 2, "Number of workers that handle events from informers")
}

type GenericNamespacedControllerOptions struct {
	GenericControllerOptions
	Namespace string
}

func BindGenericNamespacedControllerFlags(o *GenericNamespacedControllerOptions, fs ctrl.FlagSet) {
	BindGenericControllerFlags(&o.GenericControllerOptions, fs)
	fs.StringVar(&o.Namespace, "namespace", meta_v1.NamespaceAll, "Namespace to use. All namespaces are used if empty string or omitted")
}
