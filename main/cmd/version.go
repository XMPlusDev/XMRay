package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

var (
	version  = `XMRay v2604140`
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Current version of XMRay",
		Run: func(cmd *cobra.Command, args []string) {
			showVersion()
		},
	})
}

func showVersion() {
	fmt.Printf("%s\n", version)
}