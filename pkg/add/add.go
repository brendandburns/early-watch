// Package add provides the core logic for applying ChangeValidator YAML
// manifests from a file or directory path onto a Kubernetes cluster.
package add

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"

	internalapply "github.com/brendandburns/early-watch/pkg/internal/apply"
)

// Options holds the parameters for an add operation.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file. Falls back to in-cluster
	// config when empty.
	Kubeconfig string
	// Path is the path to a YAML file or a directory containing YAML files.
	Path string
}

// Run applies all ChangeValidator manifests found at opts.Path to the cluster
// described by opts.Kubeconfig, printing progress to stdout.
func Run(opts Options) error {
	cfg, err := internalapply.BuildRESTConfig(opts.Kubeconfig)
	if err != nil {
		return fmt.Errorf("building REST config: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating dynamic client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating discovery client: %w", err)
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))

	ctx := context.Background()

	files, err := collectYAMLFiles(opts.Path)
	if err != nil {
		return err
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading file %q: %w", file, err)
		}
		if err := internalapply.ApplyManifest(ctx, dynClient, mapper, data, file, nil); err != nil {
			return err
		}
	}

	return nil
}

// collectYAMLFiles returns the list of YAML files to process for the given
// path. If path is a directory, all files ending in .yaml or .yml within that
// directory (non-recursively) are returned. If path is a file, a single-element
// slice containing that path is returned.
func collectYAMLFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("accessing path %q: %w", path, err)
	}

	if !info.IsDir() {
		return []string{path}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("reading directory %q: %w", path, err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			files = append(files, filepath.Join(path, name))
		}
	}
	return files, nil
}
