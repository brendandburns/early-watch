// Package main is the entry point for the EarlyWatch audit monitor server.
//
// The audit monitor runs as a standalone HTTP server that receives Kubernetes
// audit log events (audit.k8s.io/v1 EventList) forwarded by the API server's
// audit webhook backend.  For each event it checks whether the request was a
// manual touch (i.e. originating from kubectl) matching any ManualTouchMonitor
// custom resource, and records a ManualTouchEvent when it finds a match.
package main

import (
	"flag"
	"net/http"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	sigs_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	"github.com/brendandburns/early-watch/pkg/auditmonitor"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(ewv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		listenAddr     string
		eventNamespace string
	)

	flag.StringVar(&listenAddr, "listen-address", ":8090",
		"The address the audit monitor HTTP server binds to.")
	flag.StringVar(&eventNamespace, "event-namespace", auditmonitor.DefaultEventNamespace,
		"Kubernetes namespace where ManualTouchEvent resources are created.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Build a controller-runtime client using the in-cluster or kubeconfig
	// credentials.
	cfg, err := ctrl.GetConfig()
	if err != nil {
		setupLog.Error(err, "Unable to get kubeconfig")
		os.Exit(1)
	}

	k8sClient, err := buildClient(cfg)
	if err != nil {
		setupLog.Error(err, "Unable to build Kubernetes client")
		os.Exit(1)
	}

	handler := &auditmonitor.AuditEventHandler{
		Detector: &auditmonitor.TouchDetector{Client: k8sClient},
		Recorder: &auditmonitor.TouchRecorder{
			Client:         k8sClient,
			EventNamespace: eventNamespace,
		},
	}

	mux := http.NewServeMux()
	// The Kubernetes API server POSTs audit batches to /audit by default;
	// the path is configurable via the audit webhook configuration.
	mux.Handle("/audit", handler)
	// Health/readiness probes.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	setupLog.Info("Starting EarlyWatch audit monitor", "address", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		setupLog.Error(err, "Audit monitor server exited")
		os.Exit(1)
	}
}

// buildClient creates a controller-runtime client that can read and write
// custom resources.
func buildClient(cfg *rest.Config) (sigs_client.Client, error) {
	return sigs_client.New(cfg, sigs_client.Options{Scheme: scheme})
}
