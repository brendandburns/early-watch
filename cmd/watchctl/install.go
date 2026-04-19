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

By default, install provisions the webhook TLS certificate using a self-signed
CA generated locally.  The signed certificate is stored in a Secret and the
CA certificate is injected into the ValidatingWebhookConfiguration
automatically, so no cert-manager installation is required.  Use
--no-api-server-cert-signing to skip this step and rely on cert-manager (or
another external CA) to manage the webhook certificate instead.

Pass --manual-touch to additionally install the audit-monitor CRDs, RBAC,
Deployment, and Service required for manual touch monitoring.

Pass --image-pull-secret to specify the name of an existing Kubernetes Secret
that provides credentials for pulling images from a private registry.

Example:

  watchctl install --kubeconfig ~/.kube/config
  watchctl install --kubeconfig ~/.kube/config --manual-touch
  watchctl install --kubeconfig ~/.kube/config --no-api-server-cert-signing
  watchctl install --kubeconfig ~/.kube/config --image-pull-secret my-registry-secret`,
	RunE: runInstall,
}

var installFlags struct {
	kubeconfig             string
	image                  string
	namespace              string
	manualTouch            bool
	auditMonitorImage      string
	noAPIServerCertSigning bool
	imagePullSecret        string
}

func init() {
	f := installCmd.Flags()
	f.StringVar(&installFlags.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVar(&installFlags.image, "image", defaultWebhookImageForVersion(), "Container image for the webhook Deployment.")
	f.StringVar(&installFlags.namespace, "namespace", "", "Kubernetes namespace to install EarlyWatch into. Defaults to early-watch-system.")
	f.BoolVar(&installFlags.manualTouch, "manual-touch", false, "Also install the audit-monitor components for manual touch monitoring.")
	f.StringVar(&installFlags.auditMonitorImage, "audit-monitor-image", defaultAuditMonitorImageForVersion(), "Container image for the audit-monitor Deployment. Only used with --manual-touch.")
	f.BoolVar(&installFlags.noAPIServerCertSigning, "no-api-server-cert-signing", false, "Disable automatic TLS certificate provisioning via self-signed CA. Use this when cert-manager or another external CA manages the webhook certificate.")
	f.StringVar(&installFlags.imagePullSecret, "image-pull-secret", "", "Name of an existing Kubernetes Secret used to pull images from a private registry. Added as imagePullSecrets to every managed Deployment.")
}

// defaultWebhookImageForVersion returns the default container image for the
// webhook Deployment, tagged with the current build version.
func defaultWebhookImageForVersion() string {
	return "ghcr.io/brendandburns/early-watch/webhook:" + Version
}

// defaultAuditMonitorImageForVersion returns the default container image for
// the audit-monitor Deployment, tagged with the current build version.
func defaultAuditMonitorImageForVersion() string {
	return "ghcr.io/brendandburns/early-watch/audit-monitor:" + Version
}

func runInstall(_ *cobra.Command, _ []string) error {
	opts := ewinstall.Options{
		Kubeconfig:           installFlags.kubeconfig,
		Image:                installFlags.image,
		Namespace:            installFlags.namespace,
		ManualTouchInstall:   installFlags.manualTouch,
		AuditMonitorImage:    installFlags.auditMonitorImage,
		APIServerCertSigning: !installFlags.noAPIServerCertSigning,
		ImagePullSecret:      installFlags.imagePullSecret,
	}

	if err := ewinstall.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
