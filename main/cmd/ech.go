package cmd

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/xtls/reality/hpke"
	//"github.com/xtls/xray-core/common"
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
	
	// Assuming there's a parent 'tls' command
	// tlsCmd.AddCommand(echCmd)
	// If this is a root-level command:
	//rootCmd.AddCommand(echCmd)
}

func executeECH() error {
	var kem uint16
	// Using X25519 by default
	// Uncomment below for post-quantum support when available
	// if pqSignatureSchemesEnabled {
	// 	kem = 0x30 // hpke.KEM_X25519_KYBER768_DRAFT00
	// } else {
	kem = hpke.DHKEM_X25519_HKDF_SHA256
	// }

	var configBuffer, keyBuffer []byte

	if echServerKeys == "" {
		// Generate new ECH key set
		echConfig, priv, err := tls.GenerateECHKeySet(0, echServerName, kem)
		if err != nil {
			return fmt.Errorf("failed to generate ECH key set: %w", err)
		}

		configBytes, err := tls.MarshalBinary(echConfig)
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