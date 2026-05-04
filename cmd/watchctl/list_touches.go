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
displays them in a human-readable table or as CSV.

ManualTouchEvents are created by the EarlyWatch audit monitor whenever a
manual (e.g. kubectl) change is detected on a watched resource.

Examples:

  # List all manual touches across all namespaces
  watchctl list-touches

  # List manual touches in a specific namespace
  watchctl list-touches --namespace default

  # Export manual touches as CSV
  watchctl list-touches --output csv`,
	RunE: runListTouches,
}

var listTouchesFlags struct {
	kubeconfig string
	namespace  string
	output     string
}

func init() {
	f := listTouchesCmd.Flags()
	f.StringVar(&listTouchesFlags.kubeconfig, "kubeconfig", "",
		"Path to the kubeconfig file. When empty, uses the default kubeconfig loading rules.")
	f.StringVarP(&listTouchesFlags.namespace, "namespace", "n", "",
		"Kubernetes namespace to list touches from. Lists across all namespaces when empty.")
	f.StringVarP(&listTouchesFlags.output, "output", "o", "table",
		`Output format. Supported values: "table", "csv".`)
}

func runListTouches(_ *cobra.Command, _ []string) error {
	format := ewlisttouches.OutputFormat(listTouchesFlags.output)
	if format != ewlisttouches.OutputFormatTable && format != ewlisttouches.OutputFormatCSV {
		return fmt.Errorf("unsupported output format %q; supported values: table, csv", listTouchesFlags.output)
	}
	opts := ewlisttouches.Options{
		Kubeconfig: listTouchesFlags.kubeconfig,
		Namespace:  listTouchesFlags.namespace,
		Output:     os.Stdout,
		Format:     format,
	}

	return ewlisttouches.Run(opts)
}
