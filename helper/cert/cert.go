package cert

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var defaultPath string

func New(certConf *CertConfig, path ...string) (*LegoCMD, error) {
	return newLego(certConf, "", path...)
}

// NewForNode creates a LegoCMD with a per-node DNS provider override.
// If providerName is non-empty and found in CertConfig.Providers, its CertEnv
// and provider name replace the server-level defaults for this instance only.
// Falls back to the server-level Provider + CertEnv if not found.
func NewForNode(certConf *CertConfig, providerName string, path ...string) (*LegoCMD, error) {
	return newLego(certConf, providerName, path...)
}

func newLego(certConf *CertConfig, providerName string, path ...string) (*LegoCMD, error) {
	var p string
	if len(path) > 0 && path[0] != "" {
		p = path[0]
	} else {
		configPath := os.Getenv("XRAY_LOCATION_CONFIG")
		if configPath != "" {
			p = configPath
		} else {
			p = "/etc/XMRay"
		}
	}

	defaultPath = filepath.Join(p, "cert")

	// Resolve provider: if a per-node override is given and exists, build a
	// merged CertConfig so the rest of the cert methods need no changes.
	resolved := certConf
	if providerName != "" && certConf.Providers != nil {
		if pc, ok := certConf.Providers[providerName]; ok && pc != nil {
			resolved = &CertConfig{
				Email:    certConf.Email,
				CertFile: certConf.CertFile,
				KeyFile:  certConf.KeyFile,
				Provider: providerName,
				CertEnv:  pc.CertEnv,
			}
		}
	}

	return &LegoCMD{C: resolved, path: defaultPath}, nil
}

func (l *LegoCMD) getPath() string        { return l.path }
func (l *LegoCMD) getCertConfig() *CertConfig { return l.C }

func (l *LegoCMD) DNSCert(CertMode string, CertDomain string, Email string) (CertPath string, KeyPath string, err error) {
	defer func() (string, string, error) {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknown panic")
			}
			return "", "", err
		}
		return CertPath, KeyPath, nil
	}()

	for key, value := range l.C.CertEnv {
		os.Setenv(strings.ToUpper(key), value)
	}

	CertPath, KeyPath, err = checkCertFile(CertDomain)
	if err == nil {
		return CertPath, KeyPath, err
	}

	if err = l.Run(CertMode, CertDomain, Email); err != nil {
		return "", "", err
	}
	CertPath, KeyPath, err = checkCertFile(CertDomain)
	return
}

func (l *LegoCMD) HTTPCert(CertMode string, CertDomain string, Email string) (CertPath string, KeyPath string, err error) {
	defer func() (string, string, error) {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknown panic")
			}
			return "", "", err
		}
		return CertPath, KeyPath, nil
	}()

	CertPath, KeyPath, err = checkCertFile(CertDomain)
	if err == nil {
		return CertPath, KeyPath, err
	}

	mode := strings.ToLower(CertMode)
	if mode == "http" || mode == "tls" {
		port := "80"
		if mode == "tls" {
			port = "443"
		}
		if stoppedService := getServiceOnPort(port); stoppedService != nil {
			if err := stopService(stoppedService.Name); err != nil {
				return "", "", fmt.Errorf("failed to stop %s: %v", stoppedService.Name, err)
			}
			defer func() {
				if err := startService(stoppedService.Name); err != nil {
					fmt.Printf("Failed to restart %s: %v\n", stoppedService.Name, err)
				}
			}()
			time.Sleep(2 * time.Second)
		}
	}

	if err = l.Run(CertMode, CertDomain, Email); err != nil {
		return "", "", err
	}
	CertPath, KeyPath, err = checkCertFile(CertDomain)
	return
}

func (l *LegoCMD) RenewCert(CertMode string, CertDomain string, Email string) (CertPath string, KeyPath string, ok bool, err error) {
	defer func() (string, string, bool, error) {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknown panic")
			}
			return "", "", false, err
		}
		return CertPath, KeyPath, ok, nil
	}()

	mode := strings.ToLower(CertMode)
	if mode == "http" || mode == "tls" {
		port := "80"
		if mode == "tls" {
			port = "443"
		}
		if stoppedService := getServiceOnPort(port); stoppedService != nil {
			if err := stopService(stoppedService.Name); err != nil {
				return "", "", false, fmt.Errorf("failed to stop %s: %v", stoppedService.Name, err)
			}
			defer func() {
				if err := startService(stoppedService.Name); err != nil {
					fmt.Printf("Failed to restart %s: %v\n", stoppedService.Name, err)
				}
			}()
			time.Sleep(2 * time.Second)
		}
	}

	ok, err = l.Renew(CertMode, CertDomain, Email)
	if err != nil {
		return
	}
	CertPath, KeyPath, err = checkCertFile(CertDomain)
	return
}

func checkCertFile(domain string) (string, string, error) {
	keyPath := path.Join(defaultPath, "certificates", fmt.Sprintf("%s.key", sanitizedDomain(domain)))
	certPath := path.Join(defaultPath, "certificates", fmt.Sprintf("%s.crt", sanitizedDomain(domain)))
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("cert key not found: %s", domain)
	}
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("cert file not found: %s", domain)
	}
	absKeyPath, _ := filepath.Abs(keyPath)
	absCertPath, _ := filepath.Abs(certPath)
	return absCertPath, absKeyPath, nil
}

type ServiceInfo struct {
	Name    string
	Command string
}

func getServiceOnPort(port string) *ServiceInfo {
	cmd := exec.Command("lsof", "-i", ":"+port, "-t")
	if output, err := cmd.Output(); err == nil && len(output) > 0 {
		pid := strings.TrimSpace(string(output))
		cmd = exec.Command("ps", "-p", pid, "-o", "comm=")
		if nameOutput, err := cmd.Output(); err == nil {
			return identifyService(strings.TrimSpace(string(nameOutput)))
		}
	}

	cmd = exec.Command("netstat", "-tlnp")
	if output, err := cmd.Output(); err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			if strings.Contains(line, ":"+port) {
				fields := strings.Fields(line)
				if len(fields) > 6 {
					parts := strings.Split(fields[6], "/")
					if len(parts) > 1 {
						return identifyService(parts[1])
					}
				}
			}
		}
	}

	cmd = exec.Command("ss", "-tlnp")
	if output, err := cmd.Output(); err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			if strings.Contains(line, ":"+port) && strings.Contains(line, "users:((") {
				start := strings.Index(line, "users:((\"") + 9
				if start > 9 {
					if end := strings.Index(line[start:], "\""); end > 0 {
						return identifyService(line[start : start+end])
					}
				}
			}
		}
	}

	return nil
}

func identifyService(processName string) *ServiceInfo {
	lower := strings.ToLower(processName)
	// Never stop XMRay itself — cert renewal runs inside this process.
	if strings.Contains(lower, "xmray") {
		return nil
	}
	serviceMap := map[string]string{
		"nginx":    "nginx",
		"apache2":  "apache2",
		"httpd":    "httpd",
		"caddy":    "caddy",
		"traefik":  "traefik",
		"lighttpd": "lighttpd",
		"xmbox":    "XMBox",
		"XMBox":    "XMBox",
	}
	for proc, service := range serviceMap {
		if strings.Contains(lower, proc) {
			return &ServiceInfo{Name: service, Command: processName}
		}
	}
	// Unknown process — don't stop it blindly.
	return nil
}

func stopService(serviceName string) error {
	for _, args := range [][]string{
		{"systemctl", "stop", serviceName},
		{"service", serviceName, "stop"},
		{"rc-service", serviceName, "stop"},
	} {
		if err := exec.Command(args[0], args[1:]...).Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to stop service %s", serviceName)
}

func startService(serviceName string) error {
	for _, args := range [][]string{
		{"systemctl", "start", serviceName},
		{"service", serviceName, "start"},
		{"rc-service", serviceName, "start"},
	} {
		if err := exec.Command(args[0], args[1:]...).Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to start service %s", serviceName)
}
