package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ewinstall "github.com/brendandburns/early-watch/pkg/install"
)

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove EarlyWatch infrastructure from the cluster",
	Long: `uninstall deletes every resource that "watchctl install" created:
the ChangeValidator CRD, the RBAC objects (ClusterRole, ClusterRoleBinding,
ServiceAccount), the webhook Deployment and Service, the early-watch-system
Namespace, and the ValidatingWebhookConfiguration.

Pass --manual-touch to additionally remove the audit-monitor CRDs, RBAC,
Deployment, and Service that were installed with "watchctl install --manual-touch".

Resources are removed in reverse installation order so the webhook stops
intercepting requests before lower-level objects are deleted.  Resources
that are already absent are silently skipped.

Example:

  watchctl uninstall --kubeconfig ~/.kube/config
  watchctl uninstall --kubeconfig ~/.kube/config --manual-touch`,
	RunE: runUninstall,
}

var uninstallFlags struct {
	kubeconfig  string
	namespace   string
	manualTouch bool
}

func init() {
	f := uninstallCmd.Flags()
	f.StringVar(&uninstallFlags.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVar(&uninstallFlags.namespace, "namespace", "", "Kubernetes namespace that EarlyWatch was installed into. Defaults to early-watch-system.")
	f.BoolVar(&uninstallFlags.manualTouch, "manual-touch", false, "Also remove the audit-monitor components for manual touch monitoring.")
}

func runUninstall(_ *cobra.Command, _ []string) error {
	opts := ewinstall.UninstallOptions{
		Kubeconfig:           uninstallFlags.kubeconfig,
		Namespace:            uninstallFlags.namespace,
		ManualTouchUninstall: uninstallFlags.manualTouch,
	}

	if err := ewinstall.Uninstall(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
