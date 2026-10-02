package cert

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/certificate"
	"golang.org/x/net/idna"
)

const baseCertificatesFolderName = "certificates"

type CertificatesStorage struct {
	rootPath string
	pem      bool
}

func NewCertificatesStorage(path string) *CertificatesStorage {
	return &CertificatesStorage{rootPath: filepath.Join(path, baseCertificatesFolderName)}
}

func (s *CertificatesStorage) CreateRootFolder() {
	if err := createNonExistingFolder(s.rootPath); err != nil {
		log.Panicf("Could not check/create path: %v", err)
	}
}

func (s *CertificatesStorage) GetRootPath() string { return s.rootPath }

func (s *CertificatesStorage) SaveResource(certRes *certificate.Resource) {
	domain := certRes.Domains[0]

	if err := s.WriteFile(domain, ".crt", certRes.Certificate); err != nil {
		log.Panicf("Unable to save Certificate for domain %s\n\t%v", domain, err)
	}

	if certRes.IssuerCertificate != nil {
		if err := s.WriteFile(domain, ".issuer.crt", certRes.IssuerCertificate); err != nil {
			log.Panicf("Unable to save IssuerCertificate for domain %s\n\t%v", domain, err)
		}
	}

	if certRes.PrivateKey != nil {
		if err := s.WriteFile(domain, ".key", certRes.PrivateKey); err != nil {
			log.Panicf("Unable to save PrivateKey for domain %s\n\t%v", domain, err)
		}
		if s.pem {
			if err := s.WriteFile(domain, ".pem", bytes.Join([][]byte{certRes.Certificate, certRes.PrivateKey}, nil)); err != nil {
				log.Panicf("Unable to save .pem for domain %s\n\t%v", domain, err)
			}
		}
	} else if s.pem {
		log.Panicf("Unable to save pem without private key for domain %s; are you using a CSR?", domain)
	}

	jsonBytes, err := json.MarshalIndent(certRes, "", "\t")
	if err != nil {
		log.Panicf("Unable to marshal CertResource for domain %s\n\t%v", domain, err)
	}
	if err = s.WriteFile(domain, ".json", jsonBytes); err != nil {
		log.Panicf("Unable to save CertResource for domain %s\n\t%v", domain, err)
	}
}

func (s *CertificatesStorage) ReadResource(domain string) certificate.Resource {
	raw, err := s.ReadFile(domain, ".json")
	if err != nil {
		log.Panicf("Error while loading meta data for domain %s\n\t%v", domain, err)
	}
	var resource certificate.Resource
	if err = json.Unmarshal(raw, &resource); err != nil {
		log.Panicf("Error while unmarshaling meta data for domain %s\n\t%v", domain, err)
	}
	return resource
}

func (s *CertificatesStorage) ExistsFile(domain, extension string) bool {
	if _, err := os.Stat(s.GetFileName(domain, extension)); os.IsNotExist(err) {
		return false
	} else if err != nil {
		log.Panic(err)
	}
	return true
}

func (s *CertificatesStorage) ReadFile(domain, extension string) ([]byte, error) {
	return os.ReadFile(s.GetFileName(domain, extension))
}

func (s *CertificatesStorage) GetFileName(domain, extension string) string {
	return filepath.Join(s.rootPath, sanitizedDomain(domain)+extension)
}

func (s *CertificatesStorage) ReadCertificate(domain, extension string) ([]*x509.Certificate, error) {
	content, err := s.ReadFile(domain, extension)
	if err != nil {
		return nil, err
	}
	return certcrypto.ParsePEMBundle(content)
}

func (s *CertificatesStorage) WriteFile(domain, extension string, data []byte) error {
	filePath := filepath.Join(s.rootPath, sanitizedDomain(domain)+extension)
	return os.WriteFile(filePath, data, filePerm)
}

func sanitizedDomain(domain string) string {
	safe, err := idna.ToASCII(strings.ReplaceAll(domain, "*", "_"))
	if err != nil {
		log.Panic(err)
	}
	return safe
}
