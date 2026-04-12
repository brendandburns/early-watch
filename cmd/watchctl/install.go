package main

import (
	"github.com/spf13/cobra"

	ewinstall "github.com/brendandburns/early-watch/pkg/install"
)

func init() {
	rootCmd.AddCommand(installCmd)
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install EarlyWatch onto a Kubernetes cluster",
	Long: `install applies all EarlyWatch infrastructure manifests (CRD, RBAC, and
webhook resources) onto the cluster described by --kubeconfig, using
Server-Side Apply so that the command is idempotent.

Example:

  watchctl install
  watchctl install --kubeconfig /path/to/kubeconfig
  watchctl install --image my-registry/early-watch:v1.2.3`,
	RunE: runInstall,
}

var installFlags struct {
	kubeconfig string
	image      string
}

func init() {
	f := installCmd.Flags()
	f.StringVar(&installFlags.kubeconfig, "kubeconfig", "",
		"Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVar(&installFlags.image, "image", "",
		"Container image for the webhook Deployment. Defaults to early-watch:latest.")
}

func runInstall(cmd *cobra.Command, args []string) error {
	opts := ewinstall.Options{
		Kubeconfig: installFlags.kubeconfig,
		Image:      installFlags.image,
	}

	return ewinstall.Run(opts)
}
