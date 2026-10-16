package cert

import (
	"errors"
	"io/fs"
	"log"
	"sync"
    "crypto/tls"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var saveMu sync.Mutex

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

// Hashes returns the certificate SHA-256 pin values for this generated
// certificate, formatted for sing-box TLS pinning:
//
//   - CertificateB64 -> certificate_sha256 / client_certificate_sha256
//   - PublicKeyB64   -> certificate_public_key_sha256 / client_certificate_public_key_sha256
func (c *GeneratedCert) Hashes() (CertHashes, error) {
	parsed, err := x509.ParseCertificate(c.Certificate)
	if err != nil {
		return CertHashes{}, fmt.Errorf("parse certificate: %w", err)
	}
	return CertHashesOf(parsed)
}

// Save checks if the certificate already exists and is not expired.
// If the existing certificate is still valid, it returns nil without overwriting.
// Otherwise, it writes the certificate to <pathBase>.crt and the key to <pathBase>.key
// (the `tls generate -file` behaviour). It creates parent directories if needed.

func (c *GeneratedCert) Save(pathBase string) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	reuseErr := c.checkAndReuse(pathBase)
	if reuseErr == nil {
		return nil
	}
	if !errors.Is(reuseErr, fs.ErrNotExist) { // first run is not worth a warning
		log.Printf("self-signed certificate %s regenerated: %v (clients pinning the old hash must refresh)", pathBase, reuseErr)
	}

	certPEM, keyPEM := c.ToPEM()
	if err := os.MkdirAll(filepath.Dir(pathBase), 0o755); err != nil {
		return fmt.Errorf("create certificate directory: %w", err)
	}
	// key first: whatever reloads on the .crt change then finds the matching key
	if err := writeFileAtomic(pathBase+".key", keyPEM, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if err := writeFileAtomic(pathBase+".crt", certPEM, 0o644); err != nil {
		return fmt.Errorf("write certificate: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // harmless after a successful rename
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// checkAndReuse checks if an existing certificate at pathBase is still valid.
// If it is, it reads the existing key and returns nil. If it's expired or missing,
// it returns an error to trigger a new generation.
func (c *GeneratedCert) checkAndReuse(pathBase string) error {
	fresh, err := x509.ParseCertificate(c.Certificate) // carries the SANs we want now
	if err != nil {
		return fmt.Errorf("parse new certificate: %w", err)
	}

	certPEM, err := os.ReadFile(pathBase + ".crt")
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(pathBase + ".key")
	if err != nil {
		return err
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return fmt.Errorf("invalid certificate format")
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("invalid key format")
	}
	existing, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}

	// renew a little before expiry (30 days, or 10% of lifetime for short-lived certs)
	margin := min(30*24*time.Hour, fresh.NotAfter.Sub(fresh.NotBefore)/10)
	if time.Now().Add(margin).After(existing.NotAfter) {
		return fmt.Errorf("expiring")
	}

	// the requested parameters must still match
	a, b := slices.Clone(existing.DNSNames), slices.Clone(fresh.DNSNames)
	slices.Sort(a)
	slices.Sort(b)
	if existing.IsCA != fresh.IsCA ||
		existing.Subject.CommonName != fresh.Subject.CommonName ||
		!slices.Equal(a, b) {
		return fmt.Errorf("certificate parameters changed")
	}

	// the key on disk must belong to this certificate
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("key does not match certificate: %w", err)
	}

	c.Certificate = certBlock.Bytes
	c.PrivateKey = keyBlock.Bytes // DER, like the rest of this type
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
		for _, d := range opts.Domains {
			if ip := net.ParseIP(d); ip != nil {
				template.IPAddresses = append(template.IPAddresses, ip)
			} else if d != "" {
				template.DNSNames = append(template.DNSNames, d)
			}
		}
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
