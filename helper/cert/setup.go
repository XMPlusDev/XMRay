package cert

import (
	"log"
	"os"
	"time"

	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/challenge/dns01"
	"github.com/go-acme/lego/v5/challenge/http01"
	"github.com/go-acme/lego/v5/challenge/tlsalpn01"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/providers/dns"
	"github.com/go-acme/lego/v5/registration"
	legonet "golang.org/x/crypto/acme"
)

const filePerm os.FileMode = 0o600

func setup(accountsStorage *AccountsStorage) (*Account, *lego.Client) {
	keyType := certcrypto.EC256
	privateKey := accountsStorage.GetPrivateKey(keyType)

	var account *Account
	if accountsStorage.ExistsAccountFilePath() {
		account = accountsStorage.LoadAccount(privateKey)
	} else {
		account = &Account{Email: accountsStorage.GetUserID(), key: privateKey}
	}

	return account, newClient(account)
}

func newClient(acc registration.User) *lego.Client {
	config := lego.NewConfig(acc)
	config.CADirURL = legonet.LetsEncryptURL
	config.Certificate = lego.CertificateConfig{Timeout: 30 * time.Second}
	config.UserAgent = "lego-cli/dev"

	client, err := lego.NewClient(config)
	if err != nil {
		log.Panicf("Could not create client: %v", err)
	}
	return client
}

func createNonExistingFolder(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, 0o700)
	} else if err != nil {
		return err
	}
	return nil
}

func setupChallenges(CertMode string, CertDomain string, l *LegoCMD, client *lego.Client) {
	switch CertMode {
	case "http":
		if err := client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "")); err != nil {
			log.Panic(err)
		}
	case "tls":
		if err := client.Challenge.SetTLSALPN01Provider(tlsalpn01.NewProviderServer("", "")); err != nil {
			log.Panic(err)
		}
	case "dns":
		setupDNS(l.C.Provider, client)
	default:
		log.Panic("No challenge selected. You must specify at least one challenge: `http`, `tls`, `dns`.")
	}
}

func setupDNS(p string, client *lego.Client) {
	provider, err := dns.NewDNSChallengeProviderByName(p)
	if err != nil {
		log.Panic(err)
	}
	if err := client.Challenge.SetDNS01Provider(provider,
		dns01.PropagationWait(30*time.Second, false),
		dns01.DisableAuthoritativeNssPropagationRequirement(),
	); err != nil {
		log.Panic(err)
	}
}
