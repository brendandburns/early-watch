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

Example:

  watchctl install --kubeconfig ~/.kube/config`,
	RunE: runInstall,
}

var installFlags struct {
	kubeconfig string
	image      string
	namespace  string
}

func init() {
	f := installCmd.Flags()
	f.StringVar(&installFlags.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVar(&installFlags.image, "image", "", "Container image for the webhook Deployment. Defaults to early-watch:latest.")
	f.StringVar(&installFlags.namespace, "namespace", "", "Kubernetes namespace to install EarlyWatch into. Defaults to early-watch-system.")
}

func runInstall(_ *cobra.Command, _ []string) error {
	opts := ewinstall.Options{
		Kubeconfig: installFlags.kubeconfig,
		Image:      installFlags.image,
		Namespace:  installFlags.namespace,
	}

	if err := ewinstall.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
