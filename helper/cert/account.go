package cert

import (
	"crypto"

	"github.com/go-acme/lego/v5/acme"
)

type Account struct {
	Email        string                `json:"email"`
	Registration *acme.ExtendedAccount `json:"registration"`
	key          crypto.Signer
}

func (a *Account) GetEmail() string                       { return a.Email }
func (a *Account) GetPrivateKey() crypto.Signer           { return a.key }
func (a *Account) GetRegistration() *acme.ExtendedAccount { return a.Registration }
