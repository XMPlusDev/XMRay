package cmd

import (
	"bytes"
	crand "crypto/rand"
	gtls "crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/curve25519"
)

type Response struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Model      string     `json:"model"`
	Name       string     `json:"name"`
	Key        string     `json:"key"`
	Account    Account    `json:"account"`
	WarpConfig WarpConfig `json:"config"`
	Token      string     `json:"token"`
	Warp       bool       `json:"warp_enabled"`
	Waitlist   bool       `json:"waitlist_enabled"`
	Created    string     `json:"created"`
	Updated    string     `json:"updated"`
	TOS        string     `json:"tos"`
	Place      int        `json:"place"`
	Locale     string     `json:"locale"`
	Enabled    bool       `json:"enabled"`
	InstallID  string     `json:"install_id"`
	FCMToken   string     `json:"fcm_token"`
	SerialNum  string     `json:"serial_number"`
}

type Account struct {
	ID                   string `json:"id"`
	AccountType          string `json:"account_type"`
	Created              string `json:"created"`
	Updated              string `json:"updated"`
	PremiumData          int    `json:"premium_data"`
	Quota                int    `json:"quota"`
	Usage                int    `json:"usage"`
	WarpPlus             bool   `json:"warp_plus"`
	ReferralCount        int    `json:"referral_count"`
	ReferralRenewalCount int    `json:"referral_renewal_countdown"`
	Role                 string `json:"role"`
	License              string `json:"license"`
}

type WarpConfig struct {
	ClientID  string `json:"client_id"`
	Peers     []Peer `json:"peers"`
	Interface struct {
		Addresses Addresses `json:"addresses"`
	} `json:"interface"`
	Services struct {
		HTTPProxy string `json:"http_proxy"`
	} `json:"services"`
}

type Peer struct {
	PublicKey string `json:"public_key"`
	Endpoint  struct {
		V4   string `json:"v4"`
		V6   string `json:"v6"`
		Host string `json:"host"`
	} `json:"endpoint"`
}

type Addresses struct {
	V4 string `json:"v4"`
	V6 string `json:"v6"`
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "warp",
		Short: "Generate cloudflare warp account",
		Long:  `Generate a new Cloudflare WARP account with private key, public key, and reserved IP addresses.`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := createAccount(); err != nil {
				fmt.Printf("Error creating WARP account: %v\n", err)
			}
		},
	})
}

func createAccount() error {
	privateKey, publicKey, err := GenerateKey()
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	url := "https://api.cloudflareclient.com/v0a2158/reg"
	method := "POST"

	rand.Seed(time.Now().UnixNano())
	installID := RandStringRunes(22)
	fcmtoken := RandStringRunes(134)
	payload := []byte(`{"key":"` + publicKey + `","install_id":"` + installID + `","fcm_token":"` + installID + `:APA91b` + fcmtoken + `","tos":"` + time.Now().UTC().Format("2006-01-02T15:04:05.999Z") + `","model":"Android","serial_number":"` + installID + `","locale":"zh_CN"}`)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &gtls.Config{
				MinVersion: gtls.VersionTLS12,
				MaxVersion: gtls.VersionTLS12,
			},
		},
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Add("CF-Client-Version", "a-6.10-2158")
	req.Header.Add("User-Agent", "okhttp/3.12.1")
	req.Header.Add("Content-Type", "application/json; charset=UTF-8")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", res.StatusCode, string(body))
	}

	var response Response
	err = json.Unmarshal(body, &response)
	if err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Calculate reserved bytes from client_id
	reserved, err := calculateReserved(response.WarpConfig.ClientID)
	if err != nil {
		return fmt.Errorf("failed to calculate reserved: %w", err)
	}

	// Get peer public key and endpoint
	peerPublicKey := ""
	endpoint := ""
	if len(response.WarpConfig.Peers) > 0 {
		peerPublicKey = response.WarpConfig.Peers[0].PublicKey
		endpoint = response.WarpConfig.Peers[0].Endpoint.Host
	} else {
		// Use default values if peers are not returned
		peerPublicKey = "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo="
		endpoint = "engage.cloudflareclient.com:2408"
	}

	v4 := response.WarpConfig.Interface.Addresses.V4
	v6 := response.WarpConfig.Interface.Addresses.V6

	// Print results
	fmt.Println("----", "----------------------------------")
	fmt.Println("")
	fmt.Println("Account ID:", response.Account.ID)
	fmt.Println("Account Type:", response.Account.AccountType)
	fmt.Println("License Key:", response.Account.License)
	fmt.Println("Private/Secret key:", privateKey)
	fmt.Println("Public key:", peerPublicKey)
	fmt.Println("Reserved: [", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(reserved)), ", "), "[]"), "]")
	fmt.Println("IPv4 Address:", v4)
	fmt.Println("IPv6 Address:", v6)
	fmt.Println("Endpoint:", endpoint)
	fmt.Println("")
	fmt.Println("----", "----------------------------------")

	return nil
}

func calculateReserved(clientID string) ([]int, error) {
	decoded, err := base64.StdEncoding.DecodeString(clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to decode client_id: %w", err)
	}

	hexString := hex.EncodeToString(decoded)
	reserved := []int{}

	for i := 0; i < len(hexString); i += 2 {
		hexByte := hexString[i : i+2]
		decValue, err := strconv.ParseInt(hexByte, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse hex: %w", err)
		}
		reserved = append(reserved, int(decValue))
	}

	return reserved, nil
}

func RandStringRunes(n int) string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func GenerateKey() (string, string, error) {
	b := make([]byte, 32)

	if _, err := crand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to read random bytes: %w", err)
	}

	b[0] &= 248
	b[31] &= 127
	b[31] |= 64

	var pub, priv [32]byte
	copy(priv[:], b)
	curve25519.ScalarBaseMult(&pub, &priv)

	return base64.StdEncoding.EncodeToString(priv[:]), base64.StdEncoding.EncodeToString(pub[:]), nil
}