package cmd

import (
	"github.com/spf13/cobra"
)

var tlsCmd = &cobra.Command{
	Use:   "tls",
	Short: "TLS tools",
	Long: `TLS tools for certificate management, testing, and encryption.

This command provides various TLS-related utilities including:
  - Certificate generation and management
  - TLS connection testing and diagnostics
  - Certificate hash calculation
  - ECH (Encrypted Client Hello) key generation`,
}

func init() {
	// Add TLS subcommands
	tlsCmd.AddCommand(tlsCertCmd)
	tlsCmd.AddCommand(tlsPingCmd)
	tlsCmd.AddCommand(echCmd)
	tlsCmd.AddCommand(tlsHashCmd)
	
	// Add TLS command to root
	rootCmd.AddCommand(tlsCmd)
}