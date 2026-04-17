package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ewinstall "github.com/brendandburns/early-watch/pkg/install"
)

func init() {
	rootCmd.AddCommand(installCmd)
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install EarlyWatch infrastructure onto the cluster",
	Long: `install applies the EarlyWatch CRD, RBAC, webhook Deployment, and
ValidatingWebhookConfiguration to the cluster specified by --kubeconfig (or
the in-cluster config when running inside a pod).

By default, install provisions the webhook TLS certificate using the
Kubernetes built-in CertificateSigningRequest API (certificates.k8s.io/v1).
The signed certificate is stored in a Secret and the cluster CA bundle is
injected into the ValidatingWebhookConfiguration automatically.  Use
--no-api-server-cert-signing to skip this step and rely on cert-manager (or
another external CA) to manage the webhook certificate instead.

Pass --manual-touch to additionally install the audit-monitor CRDs, RBAC,
Deployment, and Service required for manual touch monitoring.

Example:

  watchctl install --kubeconfig ~/.kube/config
  watchctl install --kubeconfig ~/.kube/config --manual-touch
  watchctl install --kubeconfig ~/.kube/config --no-api-server-cert-signing`,
	RunE: runInstall,
}

var installFlags struct {
	kubeconfig             string
	image                  string
	namespace              string
	manualTouch            bool
	auditMonitorImage      string
	noAPIServerCertSigning bool
}

func init() {
	f := installCmd.Flags()
	f.StringVar(&installFlags.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVar(&installFlags.image, "image", "", "Container image for the webhook Deployment. Defaults to early-watch:latest.")
	f.StringVar(&installFlags.namespace, "namespace", "", "Kubernetes namespace to install EarlyWatch into. Defaults to early-watch-system.")
	f.BoolVar(&installFlags.manualTouch, "manual-touch", false, "Also install the audit-monitor components for manual touch monitoring.")
	f.StringVar(&installFlags.auditMonitorImage, "audit-monitor-image", "", "Container image for the audit-monitor Deployment. Defaults to early-watch-audit-monitor:latest. Only used with --manual-touch.")
	f.BoolVar(&installFlags.noAPIServerCertSigning, "no-api-server-cert-signing", false, "Disable automatic TLS certificate provisioning via the Kubernetes CertificateSigningRequest API. Use this when cert-manager or another external CA manages the webhook certificate.")
}

func runInstall(_ *cobra.Command, _ []string) error {
	opts := ewinstall.Options{
		Kubeconfig:           installFlags.kubeconfig,
		Image:                installFlags.image,
		Namespace:            installFlags.namespace,
		ManualTouchInstall:   installFlags.manualTouch,
		AuditMonitorImage:    installFlags.auditMonitorImage,
		APIServerCertSigning: !installFlags.noAPIServerCertSigning,
	}

	if err := ewinstall.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
