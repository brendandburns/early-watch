package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ewlisttouches "github.com/brendandburns/early-watch/pkg/listtouches"
)

func init() {
	rootCmd.AddCommand(listTouchesCmd)
}

var listTouchesCmd = &cobra.Command{
	Use:   "list-touches",
	Short: "List all ManualTouchEvent resources",
	Long: `list-touches retrieves ManualTouchEvent resources from the cluster and
displays them in a human-readable table.

ManualTouchEvents are created by the EarlyWatch audit monitor whenever a
manual (e.g. kubectl) change is detected on a watched resource.

Examples:

  # List all manual touches across all namespaces
  watchctl list-touches

  # List manual touches in a specific namespace
  watchctl list-touches --namespace default`,
	RunE: runListTouches,
}

var listTouchesFlags struct {
	kubeconfig string
	namespace  string
}

func init() {
	f := listTouchesCmd.Flags()
	f.StringVar(&listTouchesFlags.kubeconfig, "kubeconfig", "",
		"Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVarP(&listTouchesFlags.namespace, "namespace", "n", "",
		"Kubernetes namespace to list touches from. Lists across all namespaces when empty.")
}

func runListTouches(_ *cobra.Command, _ []string) error {
	opts := ewlisttouches.Options{
		Kubeconfig: listTouchesFlags.kubeconfig,
		Namespace:  listTouchesFlags.namespace,
		Output:     os.Stdout,
	}

	if err := ewlisttouches.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
