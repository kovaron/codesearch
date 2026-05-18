package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "codesearch",
		Short: "Code indexing and search tool for AI agents",
	}
	root.AddCommand(
		newInitCmd(),
		newDaemonCmd(),
		newMCPCmd(),
		newExportCmd(),
		newImportCmd(),
		newBenchCmd(),
	)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
