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

Resources are removed in reverse installation order so the webhook stops
intercepting requests before lower-level objects are deleted.  Resources
that are already absent are silently skipped.

Example:

  watchctl uninstall --kubeconfig ~/.kube/config`,
	RunE: runUninstall,
}

var uninstallFlags struct {
	kubeconfig string
}

func init() {
	f := uninstallCmd.Flags()
	f.StringVar(&uninstallFlags.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
}

func runUninstall(_ *cobra.Command, _ []string) error {
	opts := ewinstall.UninstallOptions{
		Kubeconfig: uninstallFlags.kubeconfig,
	}

	if err := ewinstall.Uninstall(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
