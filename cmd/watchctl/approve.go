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
	Short: "Sign and annotate a Kubernetes resource with an approval signature",
	Long: `approve contains subcommands for approving Kubernetes resource changes.

  watchctl approve delete  – sign the resource path to pre-approve a deletion.
  watchctl approve change  – sign the merge patch and output the new resource JSON
                             with the approval annotation, ready for "kubectl apply".

Run 'watchctl approve <subcommand> --help' for details on each subcommand.`,
}

// ---------------------------------------------------------------------------
// approve delete
// ---------------------------------------------------------------------------

var approveDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Sign a Kubernetes resource path and write a delete-approval annotation",
	Long: `approve delete signs a Kubernetes resource's canonical path with a local RSA
private key and writes the resulting signature as an annotation on the resource.

The annotation is later verified by the ApprovalCheck rule in the EarlyWatch
admission webhook when a DELETE request arrives.

Example:

  watchctl approve delete \
    --private-key /path/to/private-key.pem \
    --group "" \
    --version v1 \
    --resource configmaps \
    --namespace default \
    --name my-config`,
	RunE: runApproveDelete,
}

var approveDeleteFlags struct {
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
	approveCmd.AddCommand(approveDeleteCmd)
	f := approveDeleteCmd.Flags()
	f.StringVar(&approveDeleteFlags.privateKeyPath, "private-key", "", "Path to the PEM-encoded RSA private key file (required).")
	f.StringVar(&approveDeleteFlags.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVar(&approveDeleteFlags.group, "group", "", `API group of the resource (e.g. "" for core, "apps" for Deployments).`)
	f.StringVar(&approveDeleteFlags.version, "version", "v1", `API version of the resource (e.g. "v1", "v1beta1").`)
	f.StringVar(&approveDeleteFlags.resource, "resource", "", `Plural resource name (e.g. "configmaps", "deployments") (required).`)
	f.StringVar(&approveDeleteFlags.namespace, "namespace", "", "Namespace of the resource. Leave empty for cluster-scoped resources.")
	f.StringVar(&approveDeleteFlags.name, "name", "", "Name of the resource (required).")
	f.StringVar(&approveDeleteFlags.annotationKey, "annotation-key", defaultAnnotationKey,
		"Annotation key to write the delete-approval signature to.")

	_ = approveDeleteCmd.MarkFlagRequired("private-key")
	_ = approveDeleteCmd.MarkFlagRequired("resource")
	_ = approveDeleteCmd.MarkFlagRequired("name")
}

func runApproveDelete(_ *cobra.Command, _ []string) error {
	opts := ewapprove.Options{
		PrivateKeyPath: approveDeleteFlags.privateKeyPath,
		Kubeconfig:     approveDeleteFlags.kubeconfig,
		Group:          approveDeleteFlags.group,
		Version:        approveDeleteFlags.version,
		Resource:       approveDeleteFlags.resource,
		Namespace:      approveDeleteFlags.namespace,
		Name:           approveDeleteFlags.name,
		AnnotationKey:  approveDeleteFlags.annotationKey,
	}

	if err := ewapprove.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// approve change
// ---------------------------------------------------------------------------

var approveChangeCmd = &cobra.Command{
	Use:   "change",
	Short: "Sign the merge patch for a resource modification and output the new resource JSON with the approval annotation",
	Long: `approve change fetches the current resource state from the cluster, computes
the JSON merge patch between it and the desired new state (provided as a YAML
or JSON file), signs the patch with a local RSA private key, and writes the
approval signature as a change-approval annotation into the new resource
object, which is then printed as JSON to stdout.

The output can be applied directly with "kubectl apply", which will submit
the annotated object to the cluster.  The EarlyWatch admission webhook
verifies the annotation when the UPDATE request arrives.

Example:

  watchctl approve change \
    --private-key /path/to/private-key.pem \
    --group "" \
    --version v1 \
    --resource configmaps \
    --namespace default \
    --name my-config \
    --file new-config.yaml | kubectl apply -f -`,
	RunE: runApproveChange,
}

var approveChangeFlags struct {
	privateKeyPath  string
	kubeconfig      string
	group           string
	version         string
	resource        string
	namespace       string
	name            string
	annotationKey   string
	newResourceFile string
}

func init() {
	approveCmd.AddCommand(approveChangeCmd)
	f := approveChangeCmd.Flags()
	f.StringVar(&approveChangeFlags.privateKeyPath, "private-key", "", "Path to the PEM-encoded RSA private key file (required).")
	f.StringVar(&approveChangeFlags.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file. Defaults to in-cluster config when empty.")
	f.StringVar(&approveChangeFlags.group, "group", "", `API group of the resource (e.g. "" for core, "apps" for Deployments).`)
	f.StringVar(&approveChangeFlags.version, "version", "v1", `API version of the resource (e.g. "v1", "v1beta1").`)
	f.StringVar(&approveChangeFlags.resource, "resource", "", `Plural resource name (e.g. "configmaps", "deployments") (required).`)
	f.StringVar(&approveChangeFlags.namespace, "namespace", "", "Namespace of the resource. Leave empty for cluster-scoped resources.")
	f.StringVar(&approveChangeFlags.name, "name", "", "Name of the resource (required).")
	f.StringVar(&approveChangeFlags.annotationKey, "annotation-key", ewapprove.DefaultChangeApprovalAnnotation,
		"Annotation key to write the change-approval signature to.")
	f.StringVar(&approveChangeFlags.newResourceFile, "file", "", "Path to the YAML or JSON file containing the desired new resource state (required).")

	_ = approveChangeCmd.MarkFlagRequired("private-key")
	_ = approveChangeCmd.MarkFlagRequired("resource")
	_ = approveChangeCmd.MarkFlagRequired("name")
	_ = approveChangeCmd.MarkFlagRequired("file")
}

func runApproveChange(_ *cobra.Command, _ []string) error {
	opts := ewapprove.ChangeOptions{
		PrivateKeyPath:  approveChangeFlags.privateKeyPath,
		Kubeconfig:      approveChangeFlags.kubeconfig,
		Group:           approveChangeFlags.group,
		Version:         approveChangeFlags.version,
		Resource:        approveChangeFlags.resource,
		Namespace:       approveChangeFlags.namespace,
		Name:            approveChangeFlags.name,
		AnnotationKey:   approveChangeFlags.annotationKey,
		NewResourceFile: approveChangeFlags.newResourceFile,
	}

	if err := ewapprove.RunChange(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}
