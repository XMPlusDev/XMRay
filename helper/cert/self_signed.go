package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CertOptions configures GenerateTLSCert (mirrors the flags of XMBox's
// `tls generate` command).
type CertOptions struct {
	Domains      []string      // -domain (repeatable): SANs
	CommonName   string        // -name: subject common name
	Organization string        // -org: subject organization
	IsCA         bool          // -ca: build a CA certificate
	Expire       time.Duration // -expire: validity (default 3600 days)
}

// GeneratedCert is a freshly minted certificate: ASN.1 DER certificate and
// PKCS#8 private key.
type GeneratedCert struct {
	Certificate []byte
	PrivateKey  []byte
}

// ToPEM returns the certificate and private key as PEM blocks.
func (c *GeneratedCert) ToPEM() (certPEM []byte, keyPEM []byte) {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: c.PrivateKey})
}

// JSON renders the certificate like XMBox's `tls generate -json` output:
// {"certificate": [...pem lines...], "key": [...pem lines...]}.
func (c *GeneratedCert) JSON() ([]byte, error) {
	certPEM, keyPEM := c.ToPEM()
	out := struct {
		Certificate []string `json:"certificate"`
		Key         []string `json:"key"`
	}{
		Certificate: strings.Split(strings.TrimSpace(string(certPEM)), "\n"),
		Key:         strings.Split(strings.TrimSpace(string(keyPEM)), "\n"),
	}
	b, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("XMBox: encode certificate JSON: %w", err)
	}
	return b, nil
}

// CertHashFromSelfSigned returns the xray-core cert hash (SHA-256 of the
// certificate's ASN.1 DER bytes) for a generated self-signed certificate.
// This mirrors xray-core's transport/internet/t
func CertHashFromSelfSigned(gc *GeneratedCert) ([]byte, error) {
    parsed, err := x509.ParseCertificate(gc.Certificate)
    if err != nil {
        return nil, fmt.Errorf("parse certificate: %w", err)
    }
    return CertHash(parsed), nil
}

// Save checks if the certificate already exists and is not expired.
// If the existing certificate is still valid, it returns nil without overwriting.
// Otherwise, it writes the certificate to <pathBase>.crt and the key to <pathBase>.key
// (the `tls generate -file` behaviour). It creates parent directories if needed.
func (c *GeneratedCert) Save(pathBase string) error {
	// Check if an existing certificate is present and still valid
	if err := c.checkAndReuse(pathBase); err == nil {
		return nil
	}

	certPEM, keyPEM := c.ToPEM()
	if err := os.MkdirAll(filepath.Dir(pathBase), 0o755); err != nil {
		return fmt.Errorf("XMBox: create certificate directory: %w", err)
	}
	if err := os.WriteFile(pathBase+".crt", certPEM, 0o644); err != nil {
		return fmt.Errorf("XMBox: write certificate: %w", err)
	}
	if err := os.WriteFile(pathBase+".key", keyPEM, 0o600); err != nil {
		return fmt.Errorf("XMBox: write private key: %w", err)
	}
	return nil
}

// checkAndReuse checks if an existing certificate at pathBase is still valid.
// If it is, it reads the existing key and returns nil. If it's expired or missing,
// it returns an error to trigger a new generation.
func (c *GeneratedCert) checkAndReuse(pathBase string) error {
	certPath := pathBase + ".crt"
	keyPath := pathBase + ".key"

	// Check if certificate exists
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return fmt.Errorf("not found")
	}

	// Read and parse the existing certificate
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("invalid certificate format")
	}

	existingCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}

	// Check if expired
	if time.Now().After(existingCert.NotAfter) {
		return fmt.Errorf("expired")
	}

	// Certificate is still valid - read the existing key
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read key: %w", err)
	}

	// Update the GeneratedCert with the existing values
	c.Certificate = block.Bytes
	c.PrivateKey = keyPEM
	return nil
}

// GenerateTLSCert generates a self-signed ECDSA-P256 TLS certificate.
// The logic matches cert.Generate from xray-core's
// common/protocol/tls/cert (MPL-2.0), with XMBox's flag defaults applied:
// 3600-day expiry, key usage encipherment+signature, server-auth EKU,
// and CA key usage when opts.IsCA is set.
func GenerateTLSCert(opts CertOptions) (*GeneratedCert, error) {
	expire := opts.Expire
	if expire == 0 {
		expire = 3600 * 24 * time.Hour // XMBox default: 86400h
	}

	selfKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("XMBox: generate private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("XMBox: generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(expire),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if opts.IsCA {
		template.IsCA = true
		template.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	}
	if len(opts.Domains) > 0 {
		template.DNSNames = opts.Domains
	}
	if opts.CommonName != "" {
		template.Subject = pkix.Name{CommonName: opts.CommonName}
	}
	if opts.Organization != "" {
		template.Subject.Organization = []string{opts.Organization}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &selfKey.PublicKey, selfKey)
	if err != nil {
		return nil, fmt.Errorf("XMBox: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(selfKey)
	if err != nil {
		return nil, fmt.Errorf("XMBox: marshal private key: %w", err)
	}
	return &GeneratedCert{Certificate: derBytes, PrivateKey: keyDER}, nil
}
