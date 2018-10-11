package app

import (
	"flag"
	"time"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultResyncPeriod = 20 * time.Minute
)

type GenericControllerOptions struct {
	ResyncPeriod time.Duration
	Namespace    string
	Workers      int
}

func BindGenericControllerFlags(o *GenericControllerOptions, fs *flag.FlagSet) {
	fs.DurationVar(&o.ResyncPeriod, "resync-period", defaultResyncPeriod, "Resync period for informers")
	fs.IntVar(&o.Workers, "workers", 2, "Number of workers that handle events from informers")
	fs.StringVar(&o.Namespace, "namespace", meta_v1.NamespaceAll, "Namespace to use. All namespaces are used if empty string or omitted")
}
