package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ewadd "github.com/brendandburns/early-watch/pkg/add"
)

func init() {
	rootCmd.AddCommand(addCmd)
}

var addCmd = &cobra.Command{
	Use:   "add <file-or-directory>",
	Short: "Apply a ChangeValidator from a YAML file or directory",
	Long: `add applies one or more ChangeValidator manifests to the cluster.

The argument can be a path to a single YAML file or a directory containing
multiple YAML files.  All resources found in the file(s) are applied using
Server-Side Apply, making the operation idempotent.

Examples:

  # Apply a single ChangeValidator from a file
  watchctl add config/samples/protect_service.yaml

  # Apply all ChangeValidators in a directory
  watchctl add config/samples/`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

var addFlags struct {
	kubeconfig string
}

func init() {
	addCmd.Flags().StringVar(&addFlags.kubeconfig, "kubeconfig", "",
		"Path to the kubeconfig file. Defaults to in-cluster config when empty.")
}

func runAdd(_ *cobra.Command, args []string) error {
	opts := ewadd.Options{
		Kubeconfig: addFlags.kubeconfig,
		Path:       args[0],
	}

	if err := ewadd.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
