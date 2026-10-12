package main

import (
	"flag"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	inferencev1alpha1 "github.com/llm-d/llm-d-resiliency-operator/apis/inference/v1alpha1"
	"github.com/llm-d/llm-d-resiliency-operator/internal/controller/enginefault"
	"github.com/llm-d/llm-d-resiliency-operator/internal/controller/recoveryrequest"
	"github.com/llm-d/llm-d-resiliency-operator/internal/engine/vllm"
	"github.com/llm-d/llm-d-resiliency-operator/internal/telemetry"
	"github.com/llm-d/llm-d-resiliency-operator/internal/workload/lws"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(inferencev1alpha1.AddToScheme(scheme))
}

func main() {
	klog.InitFlags(nil)
	ctrl.SetLogger(klog.Background())
	defer klog.Flush()

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var podNamespace string
	var podLabelKey string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure only one active controller manager.")
	flag.StringVar(&podNamespace, "pod-namespace", "default", "The namespace of the pod to label.")
	flag.StringVar(&podLabelKey, "pod-label-key", "llm-d.ai/inference-serving", "The label key to match the pod.")
	engineFT := enginefault.Options{
		Engines:   map[string]enginefault.EngineFactory{"vllm": &vllm.Options{}},
		Workloads: map[string]enginefault.WorkloadFactory{"lws": &lws.Options{}},
	}
	engineFT.BindFlags(flag.CommandLine)
	flag.Parse()
	if engineFT.Enabled && !enableLeaderElection {
		klog.Fatal("--enable-engine-ft needs --leader-elect")
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "llm-d-resiliency-operator-leader",
	})
	if err != nil {
		klog.Fatalf("unable to start manager: %v", err)
	}

	if err = (&recoveryrequest.Reconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		PodNamespace: podNamespace,
		PodLabelKey:  podLabelKey,
	}).SetupWithManager(mgr); err != nil {
		klog.Fatalf("unable to create controller: %v", err)
	}
	recorder := telemetry.New(ctrl.Log.WithName("engine-fault"), 10*engineFT.Config.ApplyTimeout)
	if err := engineFT.Setup(mgr, recorder); err != nil {
		klog.Fatalf("unable to set up engine FT: %v", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Fatalf("unable to set up health check: %v", err)
	}
	ready := healthz.Ping
	if engineFT.Enabled {
		ready = func(*http.Request) error { return recorder.Ready() }
	}
	if err := mgr.AddReadyzCheck("readyz", ready); err != nil {
		klog.Fatalf("unable to set up ready check: %v", err)
	}

	klog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Fatalf("problem running manager: %v", err)
	}
}
