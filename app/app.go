package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ash2k/stager"
	"github.com/atlassian/ctrl"
	"github.com/atlassian/ctrl/client"
	"github.com/atlassian/ctrl/flagutil"
	"github.com/atlassian/ctrl/logz"
	"github.com/atlassian/ctrl/process"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	core_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	core_v1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/client-go/util/workqueue"
)

const (
	defaultResyncPeriod  = 20 * time.Minute
	defaultLeaseDuration = 15 * time.Second
	defaultRenewDeadline = 10 * time.Second
	defaultRetryPeriod   = 2 * time.Second
	defaultAuxServerAddr = ":9090"
)

// See kubernetes/kubernetes/pkg/apis/componentconfig/types.go LeaderElectionConfiguration
// for leader election configuration description.
type LeaderElectionConfig struct {
	LeaderElect        bool
	LeaseDuration      time.Duration
	RenewDeadline      time.Duration
	RetryPeriod        time.Duration
	ConfigMapNamespace string
	ConfigMapName      string
}

type PrometheusRegistry interface {
	prometheus.Registerer
	prometheus.Gatherer
}

type App struct {
	Logger             *zap.Logger
	MainClient         kubernetes.Interface
	PrometheusRegistry PrometheusRegistry

	// Name is the name of the application. It must only contain alphanumeric
	// characters.
	Name                 string
	RestConfig           *rest.Config
	ResyncPeriod         time.Duration
	Namespace            string
	Controllers          []ctrl.Constructor
	Workers              int
	LeaderElectionConfig LeaderElectionConfig
	AuxListenOn          string
	Debug                bool
}

func (a *App) Run(ctx context.Context) (retErr error) {
	defer func() {
		if err := a.Logger.Sync(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to flush (AKA sync) remaining logs: %v\n", err)
		}
	}()

	// Controller
	config := &ctrl.Config{
		AppName:      a.Name,
		Namespace:    a.Namespace,
		ResyncPeriod: a.ResyncPeriod,
		Registry:     a.PrometheusRegistry,
		Logger:       a.Logger,

		RestConfig: a.RestConfig,
		MainClient: a.MainClient,
	}
	generic, err := process.NewGeneric(config,
		workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "multiqueue"),
		a.Workers, a.Controllers...)
	if err != nil {
		return err
	}

	// Auxiliary server
	auxSrv := AuxServer{
		Logger:   a.Logger,
		Addr:     a.AuxListenOn,
		Gatherer: a.PrometheusRegistry,
		Debug:    a.Debug,
	}

	// Events
	eventsScheme := runtime.NewScheme()
	// we use ConfigMapLock which emits events for ConfigMap and hence we need (only) core_v1 types for it
	if err = core_v1.AddToScheme(eventsScheme); err != nil {
		return err
	}

	// Start events recorder
	eventBroadcaster := record.NewBroadcaster()
	loggingWatch := eventBroadcaster.StartLogging(a.Logger.Sugar().Infof)
	defer loggingWatch.Stop()
	recordingWatch := eventBroadcaster.StartRecordingToSink(&core_v1client.EventSinkImpl{Interface: a.MainClient.CoreV1().Events(meta_v1.NamespaceNone)})
	defer recordingWatch.Stop()
	recorder := eventBroadcaster.NewRecorder(eventsScheme, core_v1.EventSource{Component: a.Name})

	var auxErr error
	defer func() {
		if auxErr != nil && (retErr == context.DeadlineExceeded || retErr == context.Canceled) {
			retErr = auxErr
		}
	}()

	stgr := stager.New()
	defer stgr.Shutdown()
	stage := stgr.NextStage()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stage.StartWithContext(func(metricsCtx context.Context) {
		defer cancel() // if auxSrv fails to start it signals the whole program that it should shut down
		defer logz.LogStructuredPanic()
		auxErr = auxSrv.Run(metricsCtx)
	})

	// Leader election
	if a.LeaderElectionConfig.LeaderElect {
		a.Logger.Info("Starting leader election", logz.NamespaceName(a.LeaderElectionConfig.ConfigMapNamespace))

		var startedLeading <-chan struct{}
		ctx, startedLeading, err = a.startLeaderElection(ctx, a.MainClient.CoreV1(), recorder)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-startedLeading:
		}
	}
	return generic.Run(ctx)
}

func (a *App) startLeaderElection(ctx context.Context, configMapsGetter core_v1client.ConfigMapsGetter, recorder record.EventRecorder) (context.Context, <-chan struct{}, error) {
	id, err := os.Hostname()
	if err != nil {
		return nil, nil, err
	}
	ctxRet, cancel := context.WithCancel(ctx)
	startedLeading := make(chan struct{})
	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.ConfigMapLock{
			ConfigMapMeta: meta_v1.ObjectMeta{
				Namespace: a.LeaderElectionConfig.ConfigMapNamespace,
				Name:      a.LeaderElectionConfig.ConfigMapName,
			},
			Client: configMapsGetter,
			LockConfig: resourcelock.ResourceLockConfig{
				Identity:      id + "-" + a.Name,
				EventRecorder: recorder,
			},
		},
		LeaseDuration: a.LeaderElectionConfig.LeaseDuration,
		RenewDeadline: a.LeaderElectionConfig.RenewDeadline,
		RetryPeriod:   a.LeaderElectionConfig.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(stop <-chan struct{}) {
				a.Logger.Info("Started leading")
				close(startedLeading)
			},
			OnStoppedLeading: func() {
				a.Logger.Info("Leader status lost")
				cancel()
			},
		},
	})
	if err != nil {
		cancel()
		return nil, nil, err
	}
	go func() {
		// note: because le.Run() also adds a logging panic handler panics with be logged 3 times
		defer logz.LogStructuredPanic()
		le.Run()
	}()
	return ctxRet, startedLeading, nil
}

// CancelOnInterrupt calls f when os.Interrupt or SIGTERM is received.
// It ignores subsequent interrupts on purpose - program should exit correctly after the first signal.
func CancelOnInterrupt(ctx context.Context, f context.CancelFunc) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ctx.Done():
		case <-c:
			f()
		}
	}()
}

func NewFromFlags(name string, controllers []ctrl.Constructor, flagset *flag.FlagSet, arguments []string) (*App, error) {
	a := App{
		Name:        name,
		Controllers: controllers,
	}
	for _, cntrlr := range controllers {
		cntrlr.AddFlags(flagset)
	}

	flagset.DurationVar(&a.ResyncPeriod, "resync-period", defaultResyncPeriod, "Resync period for informers")
	flagset.IntVar(&a.Workers, "workers", 2, "Number of workers that handle events from informers")
	flagset.StringVar(&a.Namespace, "namespace", meta_v1.NamespaceAll, "Namespace to use. All namespaces are used if empty string or omitted")
	flagset.BoolVar(&a.Debug, "debug", false, "Enables pprof and prefetcher dump endpoints")
	flagset.StringVar(&a.AuxListenOn, "aux-listen-on", defaultAuxServerAddr, "Auxiliary address to listen on. Used for Prometheus metrics server and pprof endpoint. Empty to disable")
	qps := flagset.Float64("api-qps", 5, "Maximum queries per second when talking to Kubernetes API")

	// This flag is off by default only because leader election package says it is ALPHA API.
	flagset.BoolVar(&a.LeaderElectionConfig.LeaderElect, "leader-elect", false, ""+
		"Start a leader election client and gain leadership before "+
		"executing the main loop. Enable this when running replicated "+
		"components for high availability")
	flagset.DurationVar(&a.LeaderElectionConfig.LeaseDuration, "leader-elect-lease-duration", defaultLeaseDuration, ""+
		"The duration that non-leader candidates will wait after observing a leadership "+
		"renewal until attempting to acquire leadership of a led but unrenewed leader "+
		"slot. This is effectively the maximum duration that a leader can be stopped "+
		"before it is replaced by another candidate. This is only applicable if leader "+
		"election is enabled")
	flagset.DurationVar(&a.LeaderElectionConfig.RenewDeadline, "leader-elect-renew-deadline", defaultRenewDeadline, ""+
		"The interval between attempts by the acting master to renew a leadership slot "+
		"before it stops leading. This must be less than or equal to the lease duration. "+
		"This is only applicable if leader election is enabled")
	flagset.DurationVar(&a.LeaderElectionConfig.RetryPeriod, "leader-elect-retry-period", defaultRetryPeriod, ""+
		"The duration the clients should wait between attempting acquisition and renewal "+
		"of a leadership. This is only applicable if leader election is enabled")
	flagset.StringVar(&a.LeaderElectionConfig.ConfigMapNamespace, "leader-elect-configmap-namespace", meta_v1.NamespaceDefault,
		"Namespace to use for leader election ConfigMap. This is only applicable if leader election is enabled")
	flagset.StringVar(&a.LeaderElectionConfig.ConfigMapName, "leader-elect-configmap-name", name+"-leader-elect",
		"ConfigMap name to use for leader election. This is only applicable if leader election is enabled")
	configFileFrom := flagset.String("client-config-from", "in-cluster",
		"Source of REST client configuration. 'in-cluster' (default), 'environment' and 'file' are valid options.")
	configFileName := flagset.String("client-config-file-name", "",
		"Load REST client configuration from the specified Kubernetes config file. This is only applicable if --client-config-from=file is set.")
	configContext := flagset.String("client-config-context", "",
		"Context to use for REST client configuration. This is only applicable if --client-config-from=file is set.")
	logEncoding := flagset.String("log-encoding", "json", `Sets the logger's encoding. Valid values are "json" and "console".`)
	loggingLevel := flagset.String("log-level", "info", `Sets the logger's output level.`)

	if err := flagutil.ValidateFlags(flagset, arguments); err != nil {
		return nil, err
	}

	if err := flagset.Parse(arguments); err != nil {
		return nil, err
	}

	config, err := client.LoadConfig(*configFileFrom, *configFileName, *configContext)
	if err != nil {
		return nil, err
	}

	config.UserAgent = name
	config.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(*qps), int(*qps*1.5))
	a.RestConfig = config

	a.Logger = logz.LoggerStr(*loggingLevel, *logEncoding)

	// Clients
	a.MainClient, err = kubernetes.NewForConfig(a.RestConfig)
	if err != nil {
		return nil, err
	}

	// Metrics
	a.PrometheusRegistry = prometheus.NewPedanticRegistry()
	err = a.PrometheusRegistry.Register(prometheus.NewProcessCollector(os.Getpid(), ""))
	if err != nil {
		return nil, err
	}
	err = a.PrometheusRegistry.Register(prometheus.NewGoCollector())
	if err != nil {
		return nil, err
	}

	return &a, nil
}
