package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// minimalKubeconfig writes a minimal kubeconfig file to dir and returns its
// path. The server points to localhost:1 which is unreachable, but the config
// parses successfully and client construction (which doesn't connect) succeeds.
func minimalKubeconfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	content := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://localhost:1
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user: {}
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing kubeconfig: %v", err)
	}
	return path
}

// TestAddCmd_Metadata verifies the command's Use and Short fields match the
// expected values so that help output stays accurate.
func TestAddCmd_Metadata(t *testing.T) {
	if addCmd.Use != "add <file-or-directory>" {
		t.Errorf("Use: got %q, want %q", addCmd.Use, "add <file-or-directory>")
	}
	if !strings.Contains(addCmd.Short, "manifest") {
		t.Errorf("Short %q should mention 'manifest'", addCmd.Short)
	}
}

// TestAddCmd_RequiresExactlyOneArg verifies that the command enforces exactly
// one positional argument.
func TestAddCmd_RequiresExactlyOneArg(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"no args", []string{}, true},
		{"two args", []string{"a", "b"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.AddCommand(addCmd)
			cmd.SetArgs(append([]string{"add"}, tc.args...))
			// Suppress output during tests.
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			err := cmd.Execute()
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestAddCmd_KubeconfigFlag verifies that the --kubeconfig flag is registered
// with the correct default value.
func TestAddCmd_KubeconfigFlag(t *testing.T) {
	f := addCmd.Flags().Lookup("kubeconfig")
	if f == nil {
		t.Fatal("--kubeconfig flag not registered")
	}
	if f.DefValue != "" {
		t.Errorf("--kubeconfig default: got %q, want empty string", f.DefValue)
	}
}

// TestRunAdd_NonExistentPath verifies that runAdd propagates an error when the
// provided path does not exist. A valid kubeconfig is given so the error
// originates from filesystem access rather than config loading.
func TestRunAdd_NonExistentPath(t *testing.T) {
	addFlags.kubeconfig = minimalKubeconfig(t)
	defer func() { addFlags.kubeconfig = "" }()

	err := runAdd(addCmd, []string{"/no/such/path/ever"})
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}

// TestRunAdd_EmptyDirectorySucceedsWithNoCluster verifies that an empty
// directory produces no error: no manifests means no cluster calls are made.
func TestRunAdd_EmptyDirectorySucceedsWithNoCluster(t *testing.T) {
	dir := t.TempDir()
	addFlags.kubeconfig = minimalKubeconfig(t)
	defer func() { addFlags.kubeconfig = "" }()

	err := runAdd(addCmd, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error for empty directory: %v", err)
	}
}

// TestRunAdd_NonYAMLFilesSkipped verifies that a directory containing only
// non-YAML files does not attempt to contact a cluster.
func TestRunAdd_NonYAMLFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	addFlags.kubeconfig = minimalKubeconfig(t)
	defer func() { addFlags.kubeconfig = "" }()

	err := runAdd(addCmd, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunAdd_WritesErrorToStderr verifies that errors from Run are written to
// stderr in addition to being returned.
func TestRunAdd_WritesErrorToStderr(t *testing.T) {
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stderr = w

	addFlags.kubeconfig = minimalKubeconfig(t)
	defer func() { addFlags.kubeconfig = "" }()
	_ = runAdd(addCmd, []string{"/no/such/path/ever"})

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	os.Stderr = origStderr

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("reading stderr: %v", err)
	}
	if !strings.Contains(buf.String(), "error:") {
		t.Errorf("stderr output %q does not contain 'error:'", buf.String())
	}
}
