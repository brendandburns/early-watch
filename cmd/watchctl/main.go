// Package main is the entry point for the watchctl command-line tool.
//
// watchctl is the EarlyWatch CLI.  Use --help to list available subcommands.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "watchctl",
	Short: "watchctl is the EarlyWatch command-line tool",
	Long: `watchctl is the EarlyWatch command-line tool for interacting with
the EarlyWatch admission webhook system.`,
}
