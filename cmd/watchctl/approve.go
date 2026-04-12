package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ewapprove "github.com/brendandburns/early-watch/pkg/approve"
)

const defaultAnnotationKey = "earlywatch.io/approved"

func init() {
	rootCmd.AddCommand(approveCmd)
}

var approveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Sign a Kubernetes resource and write an approval annotation",
	Long: `approve signs a Kubernetes resource's canonical path with a local RSA
private key and writes the resulting signature as an annotation on the resource.

The annotation is later verified by the ApprovalCheck rule in the EarlyWatch
admission webhook.

Example:

  watchctl approve \
    --private-key /path/to/private-key.pem \
    --group "" \
    --version v1 \
    --resource configmaps \
    --namespace default \
    --name my-config`,
	RunE: runApprove,
}

var approveFlags struct {
	privateKeyPath string
	kubeconfig     string
	group          string
	version        string
	resource       string
	namespace      string
	name           string
	annotationKey  string
}

func init() {
	f := approveCmd.Flags()
	f.StringVar(&approveFlags.privateKeyPath, "private-key", "", "Path to the PEM-encoded RSA private key file (required).")
	f.StringVar(&approveFlags.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVar(&approveFlags.group, "group", "", `API group of the resource (e.g. "" for core, "apps" for Deployments).`)
	f.StringVar(&approveFlags.version, "version", "v1", `API version of the resource (e.g. "v1", "v1beta1").`)
	f.StringVar(&approveFlags.resource, "resource", "", `Plural resource name (e.g. "configmaps", "deployments") (required).`)
	f.StringVar(&approveFlags.namespace, "namespace", "", "Namespace of the resource. Leave empty for cluster-scoped resources.")
	f.StringVar(&approveFlags.name, "name", "", "Name of the resource (required).")
	f.StringVar(&approveFlags.annotationKey, "annotation-key", defaultAnnotationKey,
		"Annotation key to write the signature to.")

	_ = approveCmd.MarkFlagRequired("private-key")
	_ = approveCmd.MarkFlagRequired("resource")
	_ = approveCmd.MarkFlagRequired("name")
}

func runApprove(cmd *cobra.Command, args []string) error {
	opts := ewapprove.Options{
		PrivateKeyPath: approveFlags.privateKeyPath,
		Kubeconfig:     approveFlags.kubeconfig,
		Group:          approveFlags.group,
		Version:        approveFlags.version,
		Resource:       approveFlags.resource,
		Namespace:      approveFlags.namespace,
		Name:           approveFlags.name,
		AnnotationKey:  approveFlags.annotationKey,
	}

	if err := ewapprove.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
