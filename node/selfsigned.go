package node

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/helper/cert"
)

const (
	selfSignedCAValidity   = 20 * 365 * 24 * time.Hour
	selfSignedLeafValidity = 3600 * 24 * time.Hour
)

var safeCertName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

func SelfSignedCertFile(tlsSettings *api.TlsSettings, pusher func(string, any) error, client api.API) (certFile string, keyFile string, err error) {
	if !safeCertName.MatchString(tlsSettings.ServerName) {
		return "", "", fmt.Errorf("invalid server name %q for self-signed certificate", tlsSettings.ServerName)
	}

	assetDir := os.Getenv("XRAY_LOCATION_ASSET")
	if assetDir == "" {
		assetDir = "/etc/XMRay"
	}
	pathBase := filepath.Join(assetDir, tlsSettings.ServerName)
	caBase := pathBase + "-ca" 

	domains := []string{tlsSettings.ServerName}
	if d := tlsSettings.CertDomainName; d != "" && d != tlsSettings.ServerName {
		domains = append(domains, d)
	}

	if _, err := cert.GenerateAndSaveCAChain(cert.CertOptions{
		Domains:      domains,
		CommonName:   tlsSettings.ServerName,
		Organization: "XMPlus",
		Expire:       selfSignedLeafValidity,
	}, caBase, pathBase, selfSignedCAValidity); err != nil {
		return "", "", err
	}

	fullChain := pathBase + ".fullchain.crt"
	if pusher != nil {
		reportPinnedCA(fullChain, pusher, client)
	}

	return fullChain, pathBase + ".key", nil
}

func reportPinnedCA(fullChainPath string, pusher func(string, any) error, client api.API) {
	infos, hErr := cert.HashCertsFile(fullChainPath)
	if hErr != nil || len(infos) < 2 || !infos[1].IsCA {
		log.Printf("self-signed CA hash for %s unavailable: %v", fullChainPath, hErr)
		return
	}
	hash := infos[1].SHA256
	
	if pusher != nil {
		if pErr := pusher("cert_hashes", map[string]any{"core": "xray", "pinnedPeerCertSha256": hash}); pErr == nil {
			return
		} else {
			log.Printf("cert_hashes push via websocket failed: %v, trying via api", pErr)
		}
	}
	if client != nil {
		if rErr := client.ReportCertHash(&api.CertHash{Core: "xray", PinnedPeerCertSha256: hash}); rErr != nil {
			log.Printf("cert_hashes ReportCertHash failed: %v", rErr)
		}
	}	
}
