package cert

import (
	"context"
	"fmt"
	"log"

	"github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/registration"
)

const rootPathWarningMessage = `!!!! HEADS UP !!!!

Your account credentials have been saved in your Let's Encrypt
configuration directory at "%s".

You should make a secure backup of this folder now. This
configuration directory will also contain certificates and
private keys obtained from Let's Encrypt so making regular
backups of this folder is ideal.
`

func (l *LegoCMD) Run(CertMode string, CertDomain string, Email string) error {
	accountsStorage := NewAccountsStorage(l, Email)
	account, client := setup(accountsStorage)
	setupChallenges(CertMode, CertDomain, l, client)

	registerAccount(accountsStorage, account, client)

	certsStorage := NewCertificatesStorage(l.path)
	certsStorage.CreateRootFolder()

	cert, err := obtainCertificate([]string{CertDomain}, client)
	if err != nil {
		log.Panicf("Could not obtain certificates:\n\t%v", err)
	}

	certsStorage.SaveResource(cert)
	return nil
}

// registerAccount registers the ACME account with the CA if it isn't
// registered yet (e.g. first run, or a new account created because the
// configured email address changed) and persists it to disk.
func registerAccount(accountsStorage *AccountsStorage, account *Account, client *lego.Client) {
	if account.Registration != nil {
		return
	}

	reg, err := client.Registration.Register(context.Background(), registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		log.Panicf("Could not complete registration\n\t%v", err)
	}
	account.Registration = reg
	if err = accountsStorage.Save(account); err != nil {
		log.Panic(err)
	}
	fmt.Printf(rootPathWarningMessage, accountsStorage.GetRootPath())
}

func obtainCertificate(domains []string, client *lego.Client) (*certificate.Resource, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("not a valid domain")
	}
	request := certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
		KeyType: certcrypto.EC256,
	}
	return client.Certificate.Obtain(context.Background(), request)
}
