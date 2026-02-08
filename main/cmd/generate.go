package cmd

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/protocol/tls/cert"
	"github.com/xtls/xray-core/common/task"
)

var (
	certDomainNames  []string
	certCommonName   string
	certOrganization string
	certIsCA         bool
	certJSONOutput   bool
	certFileOutput   string
	certExpire       time.Duration

	tlsCertCmd = &cobra.Command{
		Use:   "generate",
		Short: "Generate TLS certificates",
		Long: `Generate TLS certificates for testing and production use.

This command can generate both regular TLS certificates and Certificate Authority (CA) certificates.

Arguments:
  -domain         Domain name(s) for the certificate (can be specified multiple times)
  -name           Common name for the certificate
  -org            Organization name for the certificate
  -ca             Whether this certificate is a CA
  -json           Output certificate in JSON format (default: true)
  -file           File path to save the certificate (saves as .crt and .key)
  -expire         Certificate expiration duration (default: 2160h = 90 days)

Examples:
  Generate a basic certificate:
    tls generate --domain example.com

  Generate with multiple domains:
    tls generate --domain example.com --domain www.example.com --domain api.example.com

  Generate a CA certificate:
    tls generate --ca --name "My Root CA" --org "My Organization" --expire 8760h

  Save to file:
    tls generate --domain example.com --file /path/to/mycert

  Generate without JSON output:
    tls generate --domain example.com --json=false --file mycert`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := executeTLSCert(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
)

func init() {
	tlsCertCmd.Flags().StringArrayVar(&certDomainNames, "domain", nil, "Domain name for the certificate (can be specified multiple times)")
	tlsCertCmd.Flags().StringVar(&certCommonName, "name", "XMPlus Inc", "The common name of this certificate")
	tlsCertCmd.Flags().StringVar(&certOrganization, "org", "XMPlus Inc", "Organization of the certificate")
	tlsCertCmd.Flags().BoolVar(&certIsCA, "ca", false, "Whether this certificate is a CA")
	tlsCertCmd.Flags().BoolVar(&certJSONOutput, "json", true, "Print certificate in JSON format")
	tlsCertCmd.Flags().StringVar(&certFileOutput, "file", "", "Save certificate to file (creates .crt and .key files)")
	tlsCertCmd.Flags().DurationVar(&certExpire, "expire", time.Hour*24*90, "Time until the certificate expires (default: 2160h = 90 days)")
}

func executeTLSCert() error {
	var opts []cert.Option

	// CA certificate options
	if certIsCA {
		opts = append(opts, cert.Authority(certIsCA))
		opts = append(opts, cert.KeyUsage(x509.KeyUsageCertSign|x509.KeyUsageKeyEncipherment|x509.KeyUsageDigitalSignature))
	}

	// Set expiration
	opts = append(opts, cert.NotAfter(time.Now().Add(certExpire)))

	// Set common name
	opts = append(opts, cert.CommonName(certCommonName))

	// Add domain names
	if len(certDomainNames) > 0 {
		opts = append(opts, cert.DNSNames(certDomainNames...))
	}

	// Set organization
	opts = append(opts, cert.Organization(certOrganization))

	// Generate certificate
	certificate, err := cert.Generate(nil, opts...)
	if err != nil {
		return fmt.Errorf("failed to generate TLS certificate: %w", err)
	}

	// Output certificate in JSON format
	if certJSONOutput {
		if err := printJSON(certificate); err != nil {
			return fmt.Errorf("failed to output JSON: %w", err)
		}
	}

	// Save to file if specified
	if len(certFileOutput) > 0 {
		if err := printFile(certificate, certFileOutput); err != nil {
			return fmt.Errorf("failed to save certificate to file: %w", err)
		}
		fmt.Printf("\nCertificate saved to:\n")
		fmt.Printf("  Certificate: %s.crt\n", certFileOutput)
		fmt.Printf("  Private Key: %s.key\n", certFileOutput)
	}

	return nil
}

func printJSON(certificate *cert.Certificate) error {
	certPEM, keyPEM := certificate.ToPEM()
	jCert := &jsonCert{
		Certificate: strings.Split(strings.TrimSpace(string(certPEM)), "\n"),
		Key:         strings.Split(strings.TrimSpace(string(keyPEM)), "\n"),
	}

	content, err := json.MarshalIndent(jCert, "", "  ")
	if err != nil {
		return err
	}

	os.Stdout.Write(content)
	os.Stdout.WriteString("\n")
	return nil
}

func writeFile(content []byte, name string) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()

	//_, err = f.Write(content)
	//return err
	
	return common.Error2(f.Write(content))
}

func printFile(certificate *cert.Certificate, name string) error {
	certPEM, keyPEM := certificate.ToPEM()

	return task.Run(context.Background(),
		func() error {
			return writeFile(certPEM, name+".crt")
		},
		func() error {
			return writeFile(keyPEM, name+".key")
		},
	)
}

type jsonCert struct {
	Certificate []string `json:"certificate"`
	Key         []string `json:"key"`
}