package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xmplusdev/xmray/helper/cert"
)

var (
	certDomain string
	certEmail  string
	configPath string

	certCmd = &cobra.Command{
		Use:   "cert",
		Short: "Generate Certificates using Let's Encrypt",
		Long: `Certificate management commands for obtaining and renewing SSL/TLS certificates
using Let's Encrypt via HTTP challenge.`,
	}

	certObtainCmd = &cobra.Command{
		Use:   "obtain",
		Short: "Obtain a new certificate",
		Run: func(cmd *cobra.Command, args []string) {
			if err := executeCertObtain(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}

	certRenewCmd = &cobra.Command{
		Use:   "renew",
		Short: "Renew an existing certificate",
		Run: func(cmd *cobra.Command, args []string) {
			if err := executeCertRenew(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
)

func init() {
	certObtainCmd.Flags().StringVarP(&certDomain, "domain", "d", "", "Domain name for the certificate (required)")
	certObtainCmd.Flags().StringVarP(&certEmail, "email", "e", "", "Email address for Let's Encrypt notifications (required)")
	certObtainCmd.Flags().StringVarP(&configPath, "config", "c", "", "Custom config path (optional)")
	certObtainCmd.MarkFlagRequired("domain")
	certObtainCmd.MarkFlagRequired("email")

	certRenewCmd.Flags().StringVarP(&certDomain, "domain", "d", "", "Domain name for the certificate (required)")
	certRenewCmd.Flags().StringVarP(&certEmail, "email", "e", "", "Email address for Let's Encrypt notifications (required)")
	certRenewCmd.Flags().StringVarP(&configPath, "config", "c", "", "Custom config path (optional)")
	certRenewCmd.MarkFlagRequired("domain")
	certRenewCmd.MarkFlagRequired("email")

	certCmd.AddCommand(certObtainCmd)
	certCmd.AddCommand(certRenewCmd)
	rootCmd.AddCommand(certCmd)
}

func executeCertObtain() error {
	certConfig := &cert.CertConfig{Email: certEmail}
	lego, err := cert.New(certConfig)
	if err != nil {
		return fmt.Errorf("failed to create certificate client: %w", err)
	}

	fmt.Printf("Obtaining certificate for %s using HTTP challenge...\n", certDomain)
	certPath, keyPath, err := lego.HTTPCert("http", certDomain, certEmail)
	if err != nil {
		return fmt.Errorf("failed to obtain certificate: %w", err)
	}

	fmt.Println("\nCertificate obtained successfully!")
	fmt.Printf("Certificate: %s\n", certPath)
	fmt.Printf("Private Key: %s\n", keyPath)
	return nil
}

func executeCertRenew() error {
	certConfig := &cert.CertConfig{Email: certEmail}
	lego, err := cert.New(certConfig)
	if err != nil {
		return fmt.Errorf("failed to create certificate client: %w", err)
	}

	fmt.Printf("Renewing certificate for %s...\n", certDomain)
	certPath, keyPath, renewed, err := lego.RenewCert("http", certDomain, certEmail)
	if err != nil {
		return fmt.Errorf("failed to renew certificate: %w", err)
	}

	if renewed {
		fmt.Println("\nCertificate renewed successfully!")
	} else {
		fmt.Println("\nCertificate is still valid, no renewal needed")
	}
	fmt.Printf("Certificate: %s\n", certPath)
	fmt.Printf("Private Key: %s\n", keyPath)
	return nil
}
