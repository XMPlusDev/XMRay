package cmd

import (
	"fmt"
	
	"github.com/spf13/cobra"
	"github.com/xmplusdev/xmplus-server/helper/cert"
)

var (
	certDomain   string
	certEmail    string
	
	certCmd = &cobra.Command{
		Use:   "cert",
		Short: "Generate Certificates using Let's Encrypt",
		Long: `Certificate management commands for obtaining and renewing SSL/TLS certificates
using Let's Encrypt via HTTP challenge.`,
	}
	
	certObtainCmd = &cobra.Command{
		Use:   "obtain",
		Short: "Obtain a new certificate",
		Long: `Obtain a new SSL/TLS certificate for a domain using HTTP challenge.

The certificate and key files will be saved in the cert/certificates directory.`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := executeCertObtain(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	
	certRenewCmd = &cobra.Command{
		Use:   "renew",
		Short: "Renew an existing certificate",
		Long: `Renew an existing SSL/TLS certificate for a domain.

This will check if the certificate needs renewal and renew it if necessary.`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := executeCertRenew(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
)

func init() {
	// Obtain command flags
	certObtainCmd.Flags().StringVarP(&certDomain, "domain", "d", "", "Domain name for the certificate (required)")
	certObtainCmd.Flags().StringVarP(&certEmail, "email", "e", "", "Email address for Let's Encrypt notifications (required)")
	certObtainCmd.MarkFlagRequired("domain")
	certObtainCmd.MarkFlagRequired("email")
	
	// Renew command flags
	certRenewCmd.Flags().StringVarP(&certDomain, "domain", "d", "", "Domain name for the certificate (required)")
	certRenewCmd.Flags().StringVarP(&certEmail, "email", "e", "", "Email address for Let's Encrypt notifications (required)")
	certRenewCmd.MarkFlagRequired("domain")
	certRenewCmd.MarkFlagRequired("email")
	
	// Add subcommands
	certCmd.AddCommand(certObtainCmd)
	certCmd.AddCommand(certRenewCmd)
	
	// Add to root command
	rootCmd.AddCommand(certCmd)
}

func executeCertObtain() error {
	certConfig := &cert.CertConfig{
		Email:    certEmail,
	}
	
	// Create Lego client
	lego, err := cert.New(certConfig)
	if err != nil {
		return fmt.Errorf("failed to create certificate client: %w", err)
	}
	
	var certPath, keyPath string
	
	fmt.Printf("Obtaining certificate for %s using HTTP challenge...\n", certDomain)
	certPath, keyPath, err = lego.HTTPCert("http", certDomain)
	if err != nil {
		return fmt.Errorf("failed to obtain certificate: %w", err)
	}
	
	fmt.Println("\n✓ Certificate obtained successfully!")
	fmt.Printf("Certificate: %s\n", certPath)
	fmt.Printf("Private Key: %s\n", keyPath)
	
	return nil
}

func executeCertRenew() error {
	certConfig := &cert.CertConfig{
		Email:    certEmail,
	}
	
	// Create Lego client
	lego, err := cert.New(certConfig)
	if err != nil {
		return fmt.Errorf("failed to create certificate client: %w", err)
	}
	
	fmt.Printf("Renewing certificate for %s...\n", certDomain)
	
	certPath, keyPath, renewed, err := lego.RenewCert("http", certDomain)
	if err != nil {
		return fmt.Errorf("failed to renew certificate: %w", err)
	}
	
	if renewed {
		fmt.Println("\n✓ Certificate renewed successfully!")
	} else {
		fmt.Println("\n✓ Certificate is still valid, no renewal needed")
	}
	
	fmt.Printf("Certificate: %s\n", certPath)
	fmt.Printf("Private Key: %s\n", keyPath)
	
	return nil
}