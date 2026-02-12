package cmd

import (
	"crypto/ecdh"
	"crypto/hpke"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/xtls/xray-core/transport/internet/tls"
	"golang.org/x/crypto/cryptobyte"
)

var (
	echServerKeys string
	echServerName string
	echPemOutput  bool

	echCmd = &cobra.Command{
		Use:   "ech",
		Short: "Generate TLS-ECH certificates",
		Long: `Generate TLS-ECH certificates for Encrypted Client Hello.

Examples:
  Generate new ECH keys:              ech
  Set custom server name:             ech --serverName example.com
  Generate in PEM format:             ech --pem
  Restore from ECH server keys:       ech -i "ECHServerKeys (base64.StdEncoding)"`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := executeECH(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
)

func init() {
	echCmd.Flags().StringVarP(&echServerKeys, "input", "i", "", "ECHServerKeys (base64.StdEncoding)")
	echCmd.Flags().StringVar(&echServerName, "serverName", "cloudflare-ech.com", "Server name for ECH config")
	echCmd.Flags().BoolVar(&echPemOutput, "pem", false, "Output in PEM format")
}

func executeECH() error {
	var kem uint16
	// Using X25519 by default
	// Uncomment below for post-quantum support when available
	// if pqSignatureSchemesEnabled {
	// 	kem = 0x30 // hpke.KEM_X25519_KYBER768_DRAFT00
	// } else {
	kem = hpke.DHKEM(ecdh.X25519()).ID()
	// }

	var configBuffer, keyBuffer []byte

	if echServerKeys == "" {
		// Generate new ECH key set
		echConfig, priv, err := generateECHKeySet(0, echServerName, kem)
		if err != nil {
			return fmt.Errorf("failed to generate ECH key set: %w", err)
		}

		configBytes, err := marshalBinary(echConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal ECH config: %w", err)
		}

		// Build config buffer
		var b cryptobyte.Builder
		b.AddUint16LengthPrefixed(func(child *cryptobyte.Builder) {
			child.AddBytes(configBytes)
		})
		configBuffer, err = b.Bytes()
		if err != nil {
			return fmt.Errorf("failed to build config buffer: %w", err)
		}

		// Build key buffer
		var b2 cryptobyte.Builder
		b2.AddUint16(uint16(len(priv)))
		b2.AddBytes(priv)
		b2.AddUint16(uint16(len(configBytes)))
		b2.AddBytes(configBytes)
		keyBuffer, err = b2.Bytes()
		if err != nil {
			return fmt.Errorf("failed to build key buffer: %w", err)
		}
	} else {
		// Restore from existing ECH server keys
		keySetsByte, err := base64.StdEncoding.DecodeString(echServerKeys)
		if err != nil {
			return fmt.Errorf("failed to decode ECHServerKeys: %w", err)
		}

		keyBuffer = keySetsByte

		KeySets, err := tls.ConvertToGoECHKeys(keySetsByte)
		if err != nil {
			return fmt.Errorf("failed to convert ECHServerKeys: %w", err)
		}

		var b cryptobyte.Builder
		for _, keySet := range KeySets {
			b.AddUint16LengthPrefixed(func(child *cryptobyte.Builder) {
				child.AddBytes(keySet.Config)
			})
		}
		configBuffer, err = b.Bytes()
		if err != nil {
			return fmt.Errorf("failed to build config buffer: %w", err)
		}
	}

	// Output results
	if echPemOutput {
		configPEM := pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: configBuffer})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "ECH KEYS", Bytes: keyBuffer})

		os.Stdout.Write(configPEM)
		os.Stdout.Write(keyPEM)
	} else {
		fmt.Printf("ECH config list:\n%s\n\n", base64.StdEncoding.EncodeToString(configBuffer))
		fmt.Printf("ECH server keys:\n%s\n", base64.StdEncoding.EncodeToString(keyBuffer))
	}

	return nil
}

// EchConfig represents the ECH configuration structure
type EchConfig struct {
	Version              uint16
	ConfigID             uint8
	KemID                uint16
	PublicKey            []byte
	SymmetricCipherSuite []EchCipher
	MaxNameLength        uint8
	PublicName           []byte
	Extensions           []Extension
}

// EchCipher represents a cipher suite for ECH
type EchCipher struct {
	KDFID  uint16
	AEADID uint16
}

// Extension represents a TLS extension
type Extension struct {
	Type uint16
	Data []byte
}

const ExtensionEncryptedClientHello = 0xfe0d

// marshalBinary marshals an ECH config to binary format
// reference: github.com/OmarTariq612/goech
func marshalBinary(ech EchConfig) ([]byte, error) {
	var b cryptobyte.Builder
	b.AddUint16(ech.Version)
	b.AddUint16LengthPrefixed(func(child *cryptobyte.Builder) {
		child.AddUint8(ech.ConfigID)
		child.AddUint16(ech.KemID)
		child.AddUint16(uint16(len(ech.PublicKey)))
		child.AddBytes(ech.PublicKey)
		child.AddUint16LengthPrefixed(func(child *cryptobyte.Builder) {
			for _, cipherSuite := range ech.SymmetricCipherSuite {
				child.AddUint16(cipherSuite.KDFID)
				child.AddUint16(cipherSuite.AEADID)
			}
		})
		child.AddUint8(ech.MaxNameLength)
		child.AddUint8(uint8(len(ech.PublicName)))
		child.AddBytes(ech.PublicName)
		child.AddUint16LengthPrefixed(func(child *cryptobyte.Builder) {
			for _, extension := range ech.Extensions {
				child.AddUint16(extension.Type)
				child.AddBytes(extension.Data)
			}
		})
	})
	return b.Bytes()
}

// generateECHKeySet generates a new ECH key set with the specified parameters
func generateECHKeySet(configID uint8, domain string, kem uint16) (EchConfig, []byte, error) {
	config := EchConfig{
		Version:    ExtensionEncryptedClientHello,
		ConfigID:   configID,
		PublicName: []byte(domain),
		KemID:      kem,
		SymmetricCipherSuite: []EchCipher{
			{KDFID: hpke.HKDFSHA256().ID(), AEADID: hpke.AES128GCM().ID()},
			{KDFID: hpke.HKDFSHA256().ID(), AEADID: hpke.AES256GCM().ID()},
			{KDFID: hpke.HKDFSHA256().ID(), AEADID: hpke.ChaCha20Poly1305().ID()},
			{KDFID: hpke.HKDFSHA384().ID(), AEADID: hpke.AES128GCM().ID()},
			{KDFID: hpke.HKDFSHA384().ID(), AEADID: hpke.AES256GCM().ID()},
			{KDFID: hpke.HKDFSHA384().ID(), AEADID: hpke.ChaCha20Poly1305().ID()},
			{KDFID: hpke.HKDFSHA512().ID(), AEADID: hpke.AES128GCM().ID()},
			{KDFID: hpke.HKDFSHA512().ID(), AEADID: hpke.AES256GCM().ID()},
			{KDFID: hpke.HKDFSHA512().ID(), AEADID: hpke.ChaCha20Poly1305().ID()},
		},
		MaxNameLength: 0,
		Extensions:    nil,
	}

	// Generate X25519 key pair
	curve := ecdh.X25519()
	priv := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, priv)
	if err != nil {
		return config, nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}

	privKey, err := curve.NewPrivateKey(priv)
	if err != nil {
		return config, nil, fmt.Errorf("failed to create private key: %w", err)
	}

	config.PublicKey = privKey.PublicKey().Bytes()
	return config, priv, nil
}