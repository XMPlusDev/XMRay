package cert

import (
	"context"
	"crypto/x509"
	"log"
	"time"

	"github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/lego"
)

func (l *LegoCMD) Renew(CertMode string, CertDomain string, Email string) (bool, error) {
	accountsStorage := NewAccountsStorage(l, Email)
	account, client := setup(accountsStorage)
	setupChallenges(CertMode, CertDomain, l, client)

	// The configured email may have changed since the certificate was last
	// issued (new account dir, no registration on file yet). Register the
	// new account with the CA instead of failing, so renewal can proceed.
	registerAccount(accountsStorage, account, client)

	return renewForDomains(CertDomain, client, NewCertificatesStorage(l.path))
}

func renewForDomains(domain string, client *lego.Client, certsStorage *CertificatesStorage) (bool, error) {
	certificates, err := certsStorage.ReadCertificate(domain, ".crt")
	if err != nil {
		log.Panicf("Error while loading the certificate for domain %s\n\t%v", domain, err)
	}

	cert := certificates[0]
	if !needRenewal(cert, domain, 30) {
		return false, nil
	}

	timeLeft := cert.NotAfter.Sub(time.Now().UTC())
	log.Printf("[%s] acme: Trying renewal with %d hours remaining", domain, int(timeLeft.Hours()))

	certDomains := certcrypto.ExtractDomains(cert)
	request := certificate.ObtainRequest{
		Domains: certDomains,
		Bundle:  true,
		KeyType: certcrypto.EC256,
	}
	certRes, err := client.Certificate.Obtain(context.Background(), request)
	if err != nil {
		log.Panic(err)
	}

	certsStorage.SaveResource(certRes)
	return true, nil
}

func needRenewal(x509Cert *x509.Certificate, domain string, days int) bool {
	if x509Cert.IsCA {
		log.Panicf("[%s] Certificate bundle starts with a CA certificate", domain)
	}
	if days >= 0 {
		notAfter := int(time.Until(x509Cert.NotAfter).Hours() / 24.0)
		if notAfter > days {
			log.Printf("[%s] certificate expires in %d days, will renew after %d days", domain, notAfter, notAfter-days)
			return false
		}
	}
	return true
}
