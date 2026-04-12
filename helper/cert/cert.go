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

// New creates a new LegoCMD instance with optional path parameter
// Pass empty string "" for path to use default behavior
func New(certConf *CertConfig, path ...string) (*LegoCMD, error) {
	// Set default path to configPath/cert
	var p = ""
	
	// Check if optional path parameter was provided
	if len(path) > 0 && path[0] != "" {
		p = path[0]
	} else {
		configPath := os.Getenv("XRAY_LOCATION_CONFIG")
		if configPath != "" {
			p = configPath
		} else {
			p = "/etc/XMPlus"
		}
	}
	
	defaultPath = filepath.Join(p, "cert")
	lego := &LegoCMD{
		C:    certConf,
		path: defaultPath,
	}
	return lego, nil
}

func (l *LegoCMD) getPath() string {
	return l.path
}

func (l *LegoCMD) getCertConfig() *CertConfig {
	return l.C
}

// DNSCert cert a domain using DNS API
func (l *LegoCMD) DNSCert(CertMode string, CertDomain string) (CertPath string, KeyPath string, err error) {
	defer func() (string, string, error) {
		// Handle any error
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

	// Set Env for DNS configuration
	for key, value := range l.C.CertEnv {
		os.Setenv(strings.ToUpper(key), value)
	}

	// First check if the certificate exists
	CertPath, KeyPath, err = checkCertFile(CertDomain)
	if err == nil {
		return CertPath, KeyPath, err
	}

	err = l.Run(CertMode, CertDomain)
	if err != nil {
		return "", "", err
	}
	CertPath, KeyPath, err = checkCertFile(CertDomain)
	if err != nil {
		return "", "", err
	}
	return CertPath, KeyPath, nil
}

// HTTPCert cert a domain using http methods
func (l *LegoCMD) HTTPCert(CertMode string, CertDomain string) (CertPath string, KeyPath string, err error) {
	defer func() (string, string, error) {
		// Handle any error
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

	// First check if the certificate exists
	CertPath, KeyPath, err = checkCertFile(CertDomain)
	if err == nil {
		return CertPath, KeyPath, err
	}

	// Detect which port to check based on CertMode
	var port string
	if strings.ToLower(CertMode) == "http" {
		port = "80"
	} else if strings.ToLower(CertMode) == "tls" {
		port = "443"
	}

	// Check if port is in use and stop the service
	var stoppedService *ServiceInfo
	if port != "" {
		stoppedService = getServiceOnPort(port)
		if stoppedService != nil {
			if err := stopService(stoppedService.Name); err != nil {
				return "", "", fmt.Errorf("failed to stop %s: %v", stoppedService.Name, err)
			}
			
			// Ensure we restart it later
			defer func() {
				fmt.Printf("Restarting %s...\n", stoppedService.Name)
				if err := startService(stoppedService.Name); err != nil {
					fmt.Printf("Failed to restart %s: %v\n", stoppedService.Name, err)
				}
			}()
			
			// Wait for port to be released
			time.Sleep(2 * time.Second)
		}
	}

	err = l.Run(CertMode, CertDomain)
	if err != nil {
		return "", "", err
	}

	CertPath, KeyPath, err = checkCertFile(CertDomain)
	if err != nil {
		return "", "", err
	}

	return CertPath, KeyPath, nil
}

// RenewCert renew a domain cert
func (l *LegoCMD) RenewCert(CertMode string, CertDomain string) (CertPath string, KeyPath string, ok bool, err error) {
	defer func() (string, string, bool, error) {
		// Handle any error
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

	// Detect which port to check based on CertMode
	var port string
	if strings.ToLower(CertMode) == "http" {
		port = "80"
	} else if strings.ToLower(CertMode) == "tls" {
		port = "443"
	}

	// Check if port is in use and stop the service
	var stoppedService *ServiceInfo
	if port != "" {
		stoppedService = getServiceOnPort(port)
		if stoppedService != nil {
			if err := stopService(stoppedService.Name); err != nil {
				return "", "", false, fmt.Errorf("failed to stop %s: %v", stoppedService.Name, err)
			}
			
			// Ensure we restart it later
			defer func() {
				fmt.Printf("Restarting %s...\n", stoppedService.Name)
				if err := startService(stoppedService.Name); err != nil {
					fmt.Printf("Failed to restart %s: %v\n", stoppedService.Name, err)
				}
			}()
			
			// Wait for port to be released
			time.Sleep(2 * time.Second)
		}
	}

	ok, err = l.Renew(CertMode, CertDomain)
	if err != nil {
		return
	}

	CertPath, KeyPath, err = checkCertFile(CertDomain)
	if err != nil {
		return
	}

	return
}

func checkCertFile(domain string) (string, string, error) {
	keyPath := path.Join(defaultPath, "certificates", fmt.Sprintf("%s.key", sanitizedDomain(domain)))
	certPath := path.Join(defaultPath, "certificates", fmt.Sprintf("%s.crt", sanitizedDomain(domain)))
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("cert key failed: %s", domain)
	}
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("cert cert failed: %s", domain)
	}
	absKeyPath, _ := filepath.Abs(keyPath)
	absCertPath, _ := filepath.Abs(certPath)
	return absCertPath, absKeyPath, nil
}

// ServiceInfo holds information about a detected service
type ServiceInfo struct {
	Name    string
	Command string
}

// getServiceOnPort detects which service is using the specified port
func getServiceOnPort(port string) *ServiceInfo {
	// Try lsof first (most reliable)
	cmd := exec.Command("lsof", "-i", ":"+port, "-t")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		pid := strings.TrimSpace(string(output))

		// Get process name from PID
		cmd = exec.Command("ps", "-p", pid, "-o", "comm=")
		nameOutput, err := cmd.Output()
		if err == nil {
			processName := strings.TrimSpace(string(nameOutput))
			return identifyService(processName)
		}
	}

	// Fallback to netstat
	cmd = exec.Command("netstat", "-tlnp")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, ":"+port) {
				// Extract process name from netstat output
				fields := strings.Fields(line)
				if len(fields) > 6 {
					processInfo := fields[6]
					parts := strings.Split(processInfo, "/")
					if len(parts) > 1 {
						return identifyService(parts[1])
					}
				}
			}
		}
	}

	// Fallback to ss
	cmd = exec.Command("ss", "-tlnp")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, ":"+port) {
				// Extract process name
				if strings.Contains(line, "users:((") {
					start := strings.Index(line, "users:((\"") + 9
					if start > 9 {
						end := strings.Index(line[start:], "\"")
						if end > 0 {
							processName := line[start : start+end]
							return identifyService(processName)
						}
					}
				}
			}
		}
	}

	return nil
}

// identifyService maps process names to service names
func identifyService(processName string) *ServiceInfo {
	processName = strings.ToLower(processName)

	// Common web servers and their service names
	serviceMap := map[string]string{
		"nginx":    "nginx",
		"apache2":  "apache2",
		"httpd":    "httpd",
		"caddy":    "caddy",
		"traefik":  "traefik",
		"lighttpd": "lighttpd",
	}

	for proc, service := range serviceMap {
		if strings.Contains(processName, proc) {
			return &ServiceInfo{
				Name:    service,
				Command: processName,
			}
		}
	}

	// Unknown service - try to use the process name as service name
	return &ServiceInfo{
		Name:    processName,
		Command: processName,
	}
}

// stopService stops a system service
func stopService(serviceName string) error {
	// Try systemctl first (systemd)
	cmd := exec.Command("systemctl", "stop", serviceName)
	err := cmd.Run()
	if err == nil {
		return nil
	}

	// Try service command (SysV init)
	cmd = exec.Command("service", serviceName, "stop")
	err = cmd.Run()
	if err == nil {
		return nil
	}

	// Try rc-service (OpenRC)
	cmd = exec.Command("rc-service", serviceName, "stop")
	err = cmd.Run()
	if err == nil {
		return nil
	}

	return fmt.Errorf("failed to stop service using systemctl, service, or rc-service")
}

// startService starts a system service
func startService(serviceName string) error {
	// Try systemctl first (systemd)
	cmd := exec.Command("systemctl", "start", serviceName)
	err := cmd.Run()
	if err == nil {
		return nil
	}

	// Try service command (SysV init)
	cmd = exec.Command("service", serviceName, "start")
	err = cmd.Run()
	if err == nil {
		return nil
	}

	// Try rc-service (OpenRC)
	cmd = exec.Command("rc-service", serviceName, "start")
	err = cmd.Run()
	if err == nil {
		return nil
	}

	return fmt.Errorf("failed to start service using systemctl, service, or rc-service")
}