package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var saveMu sync.Mutex

var errCAChanged = errors.New("signing CA changed")

type CertOptions struct {
	Domains      []string       // -domain (repeatable): SANs (DNS names or IPs)
	CommonName   string         // -name: subject common name
	Organization string         // -org: subject organization
	IsCA         bool           // -ca: build a CA certificate
	Expire       time.Duration  // -expire: validity (default 3600 days)
	Signer       *GeneratedCert // optional: sign with this CA instead of self-signing
}

type GeneratedCert struct {
	Certificate []byte
	PrivateKey  []byte
	Chain       [][]byte
}

type CAChain struct {
	CA     *GeneratedCert
	Server *GeneratedCert
}

func (c *GeneratedCert) ToPEM() (certPEM []byte, keyPEM []byte) {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: c.PrivateKey})
}

func (c *GeneratedCert) FullChainPEM() []byte {
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate})
	for _, der := range c.Chain {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	return out
}

func (c *GeneratedCert) JSON() ([]byte, error) {
	_, keyPEM := c.ToPEM()
	out := struct {
		Certificate []string `json:"certificate"`
		Key         []string `json:"key"`
	}{
		Certificate: strings.Split(strings.TrimSpace(string(c.FullChainPEM())), "\n"),
		Key:         strings.Split(strings.TrimSpace(string(keyPEM)), "\n"),
	}
	b, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("XMRay: encode certificate JSON: %w", err)
	}
	return b, nil
}

func (c *GeneratedCert) Hashes() (CertHashes, error) {
	parsed, err := x509.ParseCertificate(c.Certificate)
	if err != nil {
		return CertHashes{}, fmt.Errorf("parse certificate: %w", err)
	}
	return CertHashesOf(parsed)
}

func (c *GeneratedCert) Save(pathBase string) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	reuseErr := c.checkAndReuse(pathBase)
	if reuseErr == nil {
		return c.saveFullChain(pathBase)
	}
	if !errors.Is(reuseErr, fs.ErrNotExist) && !errors.Is(reuseErr, errCAChanged) { 
		log.Printf("self-signed certificate %s regenerated: %v", pathBase, reuseErr)
	}

	certPEM, keyPEM := c.ToPEM()
	if err := os.MkdirAll(filepath.Dir(pathBase), 0o755); err != nil {
		return fmt.Errorf("create certificate directory: %w", err)
	}
	
	if err := writeFileAtomic(pathBase+".key", keyPEM, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if err := writeFileAtomic(pathBase+".crt", certPEM, 0o644); err != nil {
		return fmt.Errorf("write certificate: %w", err)
	}
	return c.saveFullChain(pathBase)
}

func (c *GeneratedCert) saveFullChain(pathBase string) error {
	if len(c.Chain) == 0 {
		return nil
	}
	if err := writeFileAtomic(pathBase+".fullchain.crt", c.FullChainPEM(), 0o644); err != nil {
		return fmt.Errorf("write full chain: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) 
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

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	slices.Sort(out)
	return out
}

func (c *GeneratedCert) checkAndReuse(pathBase string) error {
	fresh, err := x509.ParseCertificate(c.Certificate) 
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

	margin := min(30*24*time.Hour, fresh.NotAfter.Sub(fresh.NotBefore)/10)
	if time.Now().Add(margin).After(existing.NotAfter) {
		return errCAChanged
	}

	a, b := slices.Clone(existing.DNSNames), slices.Clone(fresh.DNSNames)
	slices.Sort(a)
	slices.Sort(b)
	if existing.IsCA != fresh.IsCA ||
		existing.Subject.CommonName != fresh.Subject.CommonName ||
		!slices.Equal(a, b) ||
		!slices.Equal(ipStrings(existing.IPAddresses), ipStrings(fresh.IPAddresses)) {
		return fmt.Errorf("certificate content changed")
	}

	if len(c.Chain) > 0 {
		parent, err := x509.ParseCertificate(c.Chain[0])
		if err != nil {
			return fmt.Errorf("parse signer certificate: %w", err)
		}
		if err := existing.CheckSignatureFrom(parent); err != nil {
			return errCAChanged 
		}
	}

	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("key does not match certificate: %w", err)
	}

	c.Certificate = certBlock.Bytes
	c.PrivateKey = keyBlock.Bytes 
	return nil
}

func loadSigner(s *GeneratedCert) (*x509.Certificate, any, error) {
	parent, err := x509.ParseCertificate(s.Certificate)
	if err != nil {
		return nil, nil, fmt.Errorf("XMRay: parse signer certificate: %w", err)
	}
	if !parent.IsCA {
		return nil, nil, errors.New("XMRay: signer certificate is not a CA")
	}
	key, err := x509.ParsePKCS8PrivateKey(s.PrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("XMRay: parse signer private key: %w", err)
	}
	return parent, key, nil
}

func GenerateTLSCert(opts CertOptions) (*GeneratedCert, error) {
	expire := opts.Expire
	if expire == 0 {
		expire = 3600 * 24 * time.Hour 
	}

	selfKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("XMRay: generate private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("XMRay: generate serial number: %w", err)
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
	for _, d := range opts.Domains {
		if d == "" {
			continue
		}
		if ip := net.ParseIP(d); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, d)
		}
	}
	if opts.CommonName != "" {
		template.Subject = pkix.Name{CommonName: opts.CommonName}
	}
	if opts.Organization != "" {
		template.Subject.Organization = []string{opts.Organization}
	}

	parent := template
	var signKey any = selfKey
	var chain [][]byte
	if opts.Signer != nil {
		parentCert, key, err := loadSigner(opts.Signer)
		if err != nil {
			return nil, err
		}
		parent, signKey = parentCert, key
		chain = append([][]byte{opts.Signer.Certificate}, opts.Signer.Chain...)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, parent, &selfKey.PublicKey, signKey)
	if err != nil {
		return nil, fmt.Errorf("XMRay: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(selfKey)
	if err != nil {
		return nil, fmt.Errorf("XMRay: marshal private key: %w", err)
	}
	return &GeneratedCert{Certificate: derBytes, PrivateKey: keyDER, Chain: chain}, nil
}

func GenerateAndSaveCAChain(opts CertOptions, caBase, serverBase string, caExpire time.Duration) (*CAChain, error) {
	if caBase == "" || serverBase == "" {
		return nil, errors.New("XMRay: caBase and serverBase are required")
	}
	if caExpire == 0 {
		caExpire = 10 * 365 * 24 * time.Hour
	}

	caName := opts.CommonName
	if caName == "" {
		caName = "XMPlus"
	}
	caName += " CA"

	ca, err := GenerateTLSCert(CertOptions{
		CommonName:   caName,
		Organization: opts.Organization,
		IsCA:         true,
		Expire:       caExpire,
	})
	if err != nil {
		return nil, err
	}
	if err := ca.Save(caBase); err != nil { 
		return nil, fmt.Errorf("save CA: %w", err)
	}

	serverOpts := opts
	serverOpts.IsCA = false
	serverOpts.Signer = ca
	if serverOpts.CommonName == "" && len(opts.Domains) > 0 {
		serverOpts.CommonName = opts.Domains[0]
	}
	server, err := GenerateTLSCert(serverOpts)
	if err != nil {
		return nil, err
	}
	if err := server.Save(serverBase); err != nil {
		return nil, fmt.Errorf("save server certificate: %w", err)
	}
	return &CAChain{CA: ca, Server: server}, nil
}