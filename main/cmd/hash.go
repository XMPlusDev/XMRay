package cmd

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	. "github.com/xtls/xray-core/transport/internet/tls"
)

var (
	hashCertPath string

	tlsHashCmd = &cobra.Command{
		Use:   "hash",
		Short: "Calculate TLS certificate hash",
		Long: `Calculate TLS certificate hash.

This command reads a certificate file (PEM or CRT format) and calculates
the SHA256 hash for each certificate in the chain:
  - Leaf certificate hash

The leaf certificate hash is particularly useful for REALITY protocol
fingerprint configuration.

Examples:
  Calculate hash for default cert:     hash
  Calculate hash for custom cert:      hash --cert /etc/XMPlus/cert/certificates/example.com.crt
  Calculate for Let's Encrypt cert:    hash --cert /etc/letsencrypt/live/example.com/fullchain.pem`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := executeTLSHash(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
)

func init() {
	tlsHashCmd.Flags().StringVar(&hashCertPath, "cert", "fullchain.pem", "The file path of the certificate")
}

func executeTLSHash() error {
	// Check if file exists
	if _, err := os.Stat(hashCertPath); os.IsNotExist(err) {
		return fmt.Errorf("certificate file not found: %s", hashCertPath)
	}

	// Read certificate file
	certContent, err := os.ReadFile(hashCertPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	var certs []*x509.Certificate

	// Parse PEM format
	if bytes.Contains(certContent, []byte("BEGIN")) {
		for {
			block, remain := pem.Decode(certContent)
			if block == nil {
				break
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return fmt.Errorf("unable to decode certificate: %w", err)
			}
			certs = append(certs, cert)
			certContent = remain
		}
	} else {
		// Parse DER format
		certs, err = x509.ParseCertificates(certContent)
		if err != nil {
			return fmt.Errorf("unable to parse certificates: %w", err)
		}
	}

	// Check if any certificates were found
	if len(certs) == 0 {
		return fmt.Errorf("no certificates found in file")
	}

	// Print certificate hashes in tabular format
	tabWriter := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, cert := range certs {
		hash := GenerateCertHashHex(cert)
		if i == 0 {
			fmt.Fprintf(tabWriter, "Leaf SHA256:\t%s\n", hash)
		} else {
			fmt.Fprintf(tabWriter, "CA <%s> SHA256:\t%s\n", cert.Subject.CommonName, hash)
		}
	}
	tabWriter.Flush()

	return nil
}