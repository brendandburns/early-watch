// Package listtouches provides the core logic for listing ManualTouchEvent
// resources from a Kubernetes cluster.
package listtouches

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
	internalapply "github.com/brendandburns/early-watch/pkg/internal/apply"
)

// Options holds the parameters for a list-touches operation.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file. Falls back to in-cluster
	// config when empty.
	Kubeconfig string

	// Namespace restricts the listing to a specific namespace.
	// An empty string lists across all namespaces.
	Namespace string

	// Output is the writer to print results to.
	Output io.Writer
}

// BuildScheme returns a *runtime.Scheme that includes the earlywatch.io
// v1alpha1 types.
func BuildScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	if err := ewv1alpha1.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("registering earlywatch scheme: %w", err)
	}
	return s, nil
}

// BuildClient creates a controller-runtime client configured for the
// earlywatch.io API types, using the given kubeconfig path.
func BuildClient(kubeconfig string) (k8sclient.Client, error) {
	cfg, err := internalapply.BuildRESTConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("building REST config: %w", err)
	}

	s, err := BuildScheme()
	if err != nil {
		return nil, err
	}

	c, err := k8sclient.New(cfg, k8sclient.Options{Scheme: s})
	if err != nil {
		return nil, fmt.Errorf("creating Kubernetes client: %w", err)
	}
	return c, nil
}

// List fetches all ManualTouchEvent resources visible to c. When namespace
// is non-empty only that namespace is searched; otherwise all namespaces
// are searched.
func List(ctx context.Context, c k8sclient.Client, namespace string) (*ewv1alpha1.ManualTouchEventList, error) {
	list := &ewv1alpha1.ManualTouchEventList{}
	var opts []k8sclient.ListOption
	if namespace != "" {
		opts = append(opts, k8sclient.InNamespace(namespace))
	}
	if err := c.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("listing ManualTouchEvents: %w", err)
	}
	return list, nil
}

// Run builds a Kubernetes client, lists ManualTouchEvent resources, and
// writes the results as a human-readable table to opts.Output.
func Run(opts Options) error {
	c, err := BuildClient(opts.Kubeconfig)
	if err != nil {
		return err
	}

	list, err := List(context.Background(), c, opts.Namespace)
	if err != nil {
		return err
	}

	PrintTable(opts.Output, list.Items)
	return nil
}

// PrintTable writes a human-readable table of ManualTouchEvents to w.
func PrintTable(w io.Writer, events []ewv1alpha1.ManualTouchEvent) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tNAME\tUSER\tOPERATION\tRESOURCE\tRESOURCE NAME\tAGE")
	for _, e := range events {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.Namespace,
			e.Name,
			e.Spec.User,
			e.Spec.Operation,
			e.Spec.Resource,
			e.Spec.ResourceName,
			formatAge(time.Since(e.CreationTimestamp.Time)),
		)
	}
	tw.Flush()
}

// formatAge returns a short human-readable string representing d.
func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
