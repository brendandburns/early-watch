// Package main is the entry point for the EarlyWatch admission webhook server.
package main

import (
	"flag"
	"os"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	ewwebhook "github.com/brendandburns/early-watch/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(ewv1alpha1.AddToScheme(scheme))
	utilruntime.Must(admissionv1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		certDir              string
		webhookPort          int
		enableLeaderElection bool
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080",
		"The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081",
		"The address the health probe endpoint binds to.")
	flag.StringVar(&certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs",
		"Directory holding the TLS certificate and key used by the webhook server.")
	flag.IntVar(&webhookPort, "webhook-port", 9443,
		"Port the admission webhook server listens on.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for the controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

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
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "early-watch.io",
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    webhookPort,
			CertDir: certDir,
		}),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Build the dynamic client for ExistingResources checks.
	dynamicClient, err := buildDynamicClient()
	if err != nil {
		setupLog.Error(err, "unable to build dynamic client")
		os.Exit(1)
	}

	// Register the admission webhook handler.
	decoder := admission.NewDecoder(scheme)
	handler := &ewwebhook.AdmissionHandler{
		Client:        mgr.GetClient(),
		DynamicClient: dynamicClient,
		Decoder:       decoder,
	}

	// Register with the webhook server; the path must match
	// the ValidatingWebhookConfiguration's clientConfig.service.path.
	mgr.GetWebhookServer().Register("/validate", &webhook.Admission{Handler: handler})

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting EarlyWatch admission webhook server")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// buildDynamicClient creates a dynamic Kubernetes client from the in-cluster
// or kubeconfig credentials.
func buildDynamicClient() (dynamic.Interface, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}
