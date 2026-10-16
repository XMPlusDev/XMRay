package cert

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
)

// CertHash returns the SHA-256 hash of the certificate's ASN.1 DER bytes.
// Mirrors GenerateCertHash from xray-core's transport/internet/tls (pin.go);
// this is the value REALITY "pinning" / fingerprint configs expect.
func CertHash(cert *x509.Certificate) []byte {
	sum := sha256.Sum256(cert.Raw)
	return sum[:]
}

// CertHashHex returns CertHash as a lowercase hex string (the value XMRay's
// `tls hash` command prints).
func CertHashHex(cert *x509.Certificate) string {
	return hex.EncodeToString(CertHash(cert))
}

// CertInfo summarises one certificate of a chain.
type CertInfo struct {
	CommonName         string   `json:"common_name"`
	SHA256             string   `json:"sha256"`
	DNSNames           []string `json:"dns_names,omitempty"`
	SignatureAlgorithm string   `json:"signature_algorithm"`
	PublicKeyAlgorithm string   `json:"public_key_algorithm"`
	IsCA               bool     `json:"is_ca"`
	RawLen             int      `json:"raw_len"`
}

// CertInfoOf builds a CertInfo for one parsed certificate.
func CertInfoOf(cert *x509.Certificate) CertInfo {
	return CertInfo{
		CommonName:         cert.Subject.CommonName,
		SHA256:             CertHashHex(cert),
		DNSNames:           cert.DNSNames,
		SignatureAlgorithm: cert.SignatureAlgorithm.String(),
		PublicKeyAlgorithm: cert.PublicKeyAlgorithm.String(),
		IsCA:               cert.IsCA,
		RawLen:             len(cert.Raw),
	}
}

// ParseCerts parses certificate data that is either PEM (chain) or DER.
// It errors when no certificate can be found, like XMRay's `tls hash` command.
func ParseCerts(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate

	if bytes.Contains(data, []byte("BEGIN")) {
		rest := data
		for {
			block, remain := pem.Decode(rest)
			if block == nil {
				break
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("xmfunc: decode certificate: %w", err)
			}
			certs = append(certs, cert)
			rest = remain
		}
	} else {
		var err error
		certs, err = x509.ParseCertificates(data)
		if err != nil {
			return nil, fmt.Errorf("xmfunc: parse certificates: %w", err)
		}
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("xmfunc: no certificates found")
	}
	return certs, nil
}

// HashCertsFile mirrors XMRay's `tls hash` command: read a PEM/DER certificate
// file (e.g. fullchain.pem) and return its chain with SHA-256 hashes, leaf first.
func HashCertsFile(path string) ([]CertInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("xmfunc: read certificate file: %w", err)
	}
	certs, err := ParseCerts(data)
	if err != nil {
		return nil, err
	}
	infos := make([]CertInfo, 0, len(certs))
	for _, cert := range certs {
		infos = append(infos, CertInfoOf(cert))
	}
	return infos, nil
}

// CertHashes holds the two certificate pin values for TLS pinning:
//   - Certificate -> SHA-256 of the certificate's ASN.1 DER bytes
//   - PublicKey   -> SHA-256 of the certificate's SubjectPublicKeyInfo DER bytes
type CertHashes struct {
	Certificate    []byte
	PublicKey      []byte
	CertificateB64 string
	PublicKeyB64   string
	CertificateHex string
	PublicKeyHex   string
}

// CertHashesOf computes the pin hashes for one certificate.
func CertHashesOf(cert *x509.Certificate) (CertHashes, error) {
	certHash := sha256.Sum256(cert.Raw)
	pubKeyHash, err := publicKeySHA256(cert)
	if err != nil {
		return CertHashes{}, err
	}
	return CertHashes{
		Certificate:    certHash[:],
		PublicKey:      pubKeyHash[:],
		CertificateB64: base64.StdEncoding.EncodeToString(certHash[:]),
		PublicKeyB64:   base64.StdEncoding.EncodeToString(pubKeyHash[:]),
		CertificateHex: hex.EncodeToString(certHash[:]),
		PublicKeyHex:   hex.EncodeToString(pubKeyHash[:]),
	}, nil
}

// publicKeySHA256 returns the SHA-256 hash of the certificate's
// SubjectPublicKeyInfo DER bytes.
func publicKeySHA256(cert *x509.Certificate) ([]byte, error) {
	pubKeyDER, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	sum := sha256.Sum256(pubKeyDER)
	return sum[:], nil
}
