package cert

import (
	"context"
	"crypto"
	"encoding/json"
	"encoding/pem"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/lego"
	legonet "golang.org/x/crypto/acme"
)

const (
	baseAccountsRootFolderName = "accounts"
	baseKeysFolderName         = "keys"
	accountFileName            = "account.json"
)

type AccountsStorage struct {
	userID          string
	rootPath        string
	rootUserPath    string
	keysPath        string
	accountFilePath string
}

func NewAccountsStorage(l *LegoCMD, certEmail string) *AccountsStorage {
	email := certEmail
	if email == "" {
		email = l.C.Email
	}

	serverURL, err := url.Parse(legonet.LetsEncryptURL)
	if err != nil {
		log.Panic(err)
	}

	rootPath := filepath.Join(l.path, baseAccountsRootFolderName)
	serverPath := strings.NewReplacer(":", "_", "/", string(os.PathSeparator)).Replace(serverURL.Host)
	accountsPath := filepath.Join(rootPath, serverPath)
	rootUserPath := filepath.Join(accountsPath, email)

	return &AccountsStorage{
		userID:          email,
		rootPath:        rootPath,
		rootUserPath:    rootUserPath,
		keysPath:        filepath.Join(rootUserPath, baseKeysFolderName),
		accountFilePath: filepath.Join(rootUserPath, accountFileName),
	}
}

func (s *AccountsStorage) ExistsAccountFilePath() bool {
	accountFile := filepath.Join(s.rootUserPath, accountFileName)
	if _, err := os.Stat(accountFile); os.IsNotExist(err) {
		return false
	} else if err != nil {
		log.Panic(err)
	}
	return true
}

func (s *AccountsStorage) GetRootPath() string    { return s.rootPath }
func (s *AccountsStorage) GetRootUserPath() string { return s.rootUserPath }
func (s *AccountsStorage) GetUserID() string       { return s.userID }

func (s *AccountsStorage) Save(account *Account) error {
	jsonBytes, err := json.MarshalIndent(account, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(s.accountFilePath, jsonBytes, filePerm)
}

func (s *AccountsStorage) LoadAccount(privateKey crypto.Signer) *Account {
	fileBytes, err := os.ReadFile(s.accountFilePath)
	if err != nil {
		log.Panicf("Could not load file for account %s: %v", s.userID, err)
	}

	var account Account
	if err = json.Unmarshal(fileBytes, &account); err != nil {
		log.Panicf("Could not parse file for account %s: %v", s.userID, err)
	}
	account.key = privateKey

	if account.Registration == nil || account.Registration.Status == "" {
		reg, err := tryRecoverRegistration(privateKey)
		if err != nil {
			log.Panicf("Could not load account for %s. Registration is nil: %#v", s.userID, err)
		}
		account.Registration = reg
		if err = s.Save(&account); err != nil {
			log.Panicf("Could not save account for %s: %#v", s.userID, err)
		}
	}

	return &account
}

func (s *AccountsStorage) GetPrivateKey(keyType certcrypto.KeyType) crypto.Signer {
	accKeyPath := filepath.Join(s.keysPath, s.userID+".key")

	if _, err := os.Stat(accKeyPath); os.IsNotExist(err) {
		log.Printf("No key found for account %s. Generating a %s key.", s.userID, keyType)
		s.createKeysFolder()

		privateKey, err := generatePrivateKey(accKeyPath, keyType)
		if err != nil {
			log.Panicf("Could not generate private key for account %s: %v", s.userID, err)
		}
		log.Printf("Saved key to %s", accKeyPath)
		return privateKey
	}

	privateKey, err := loadPrivateKey(accKeyPath)
	if err != nil {
		log.Panicf("Could not load private key from file %s: %v", accKeyPath, err)
	}
	return privateKey
}

func (s *AccountsStorage) createKeysFolder() {
	if err := createNonExistingFolder(s.keysPath); err != nil {
		log.Panicf("Could not check/create directory for account %s: %v", s.userID, err)
	}
}

func generatePrivateKey(file string, keyType certcrypto.KeyType) (crypto.Signer, error) {
	privateKey, err := certcrypto.GeneratePrivateKey(keyType)
	if err != nil {
		return nil, err
	}

	certOut, err := os.Create(file)
	if err != nil {
		return nil, err
	}
	defer certOut.Close()

	pemKey := certcrypto.PEMBlock(privateKey)
	if err = pem.Encode(certOut, pemKey); err != nil {
		return nil, err
	}
	return privateKey, nil
}

func loadPrivateKey(file string) (crypto.Signer, error) {
	keyBytes, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return certcrypto.ParsePEMPrivateKey(keyBytes)
}

func tryRecoverRegistration(privateKey crypto.Signer) (*acme.ExtendedAccount, error) {
	config := lego.NewConfig(&Account{key: privateKey})
	config.CADirURL = legonet.LetsEncryptURL
	config.UserAgent = "lego-cli/dev"

	client, err := lego.NewClient(config)
	if err != nil {
		return nil, err
	}
	return client.Registration.ResolveAccountByKey(context.Background())
}
