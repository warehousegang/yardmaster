package main

import (
	"flag"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	yardv1alpha1 "github.com/warehousegang/yardmaster/api/v1alpha1"
	yardcontroller "github.com/warehousegang/yardmaster/internal/controller"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(yardv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var findingNamespace string
	var leaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&findingNamespace, "finding-namespace", yardcontroller.DefaultFindingNamespace, "Namespace where DispatchFinding resources are written.")
	flag.BoolVar(&leaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElection,
		LeaderElectionID:       "yardmaster.dev",
	})
	if err != nil {
		ctrl.Log.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&yardcontroller.PendingPodReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		FindingNamespace: findingNamespace,
	}).SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create pending pod controller")
		os.Exit(1)
	}

	if err := (&yardcontroller.RequestCoverageReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		FindingNamespace: findingNamespace,
	}).SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create request coverage controller")
		os.Exit(1)
	}

	if err := (&yardcontroller.TrackSummaryReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		FindingNamespace: findingNamespace,
	}).SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create track summary controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctrl.Log.Info("starting yardmaster")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "problem running manager")
		os.Exit(1)
	}
}
