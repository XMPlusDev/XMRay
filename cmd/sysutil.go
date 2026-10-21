package cmd

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	xmrayRepo       = "XMPlusDev/XMRay"
	xmrayInstallDir = "/usr/local/XMRay"
	xmrayConfigDir  = "/etc/XMRay"
	xmrayBinDir     = "/usr/bin"
	systemdDir      = "/etc/systemd/system"
	serviceUnitFile = "/etc/systemd/system/XMRay.service"
	serviceName     = "XMRay"
	xmrayBinName    = "XMRay"
	xmrayLinkName   = "xmray"
)

// checkRoot returns an error if the current process is not running as root.
func checkRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}
	return nil
}

// detectOS reads /etc/os-release and related files to identify the OS family.
func detectOS() (string, error) {
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ID=") {
				release := strings.Trim(strings.TrimPrefix(line, "ID="), `"'`)
				if release != "" {
					return release, nil
				}
			}
		}
	}

	if _, err := os.Stat("/etc/redhat-release"); err == nil {
		return "centos", nil
	}

	if data, err := os.ReadFile("/proc/version"); err == nil {
		content := strings.ToLower(string(data))
		switch {
		case strings.Contains(content, "debian"):
			return "debian", nil
		case strings.Contains(content, "ubuntu"):
			return "ubuntu", nil
		case strings.Contains(content, "centos") || strings.Contains(content, "red hat") || strings.Contains(content, "redhat"):
			return "centos", nil
		}
	}

	return "", fmt.Errorf("system version not detected, please contact the script author")
}

// detectOSVersion returns the major version number from /etc/os-release.
func detectOSVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "VERSION_ID=") {
			v := strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"'`)
			if parts := strings.SplitN(v, ".", 2); len(parts) > 0 {
				return parts[0]
			}
			return v
		}
	}
	return ""
}

// detectArch returns the kernel architecture string used in download URLs.
func detectArch() string {
	switch runtime.GOARCH {
	case "386", "i686":
		return "32"
	case "amd64", "x86_64":
		return "64"
	case "arm64":
		return "arm64-v8a"
	default:
		return runtime.GOARCH
	}
}

// runCommand executes a command and returns its combined output.
func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// runCommandWithOutput executes a command, streaming stdout/stderr to the terminal.
func runCommandWithOutput(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// isSystemdAvailable reports whether systemd is the init system.
func isSystemdAvailable() bool {
	_, err := os.Stat("/etc/systemd/system/")
	return err == nil
}

// isXMRayInstalled reports whether XMRay appears to be installed.
func isXMRayInstalled() bool {
	if _, err := os.Stat(serviceUnitFile); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(xmrayBinDir, xmrayBinName)); err == nil {
		return true
	}
	return false
}

// checkInstalled returns an error if XMRay is not installed.
func checkInstalled() error {
	if !isXMRayInstalled() {
		return fmt.Errorf("XMRay is not installed, please run [ xmray install ] first")
	}
	return nil
}

// confirm prompts the user for a yes/no confirmation.
func confirm(prompt string, defaultYes bool) (bool, error) {
	defaultText := "N"
	if defaultYes {
		defaultText = "Y"
	}
	fmt.Printf("%s [y/%s]: ", prompt, defaultText)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultYes, nil
	}
	return input == "y" || input == "yes", nil
}

// fetchLatestVersion queries the GitHub API for the latest release tag.
func fetchLatestVersion() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", xmrayRepo)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "xmray-updater")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to query GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d (possibly rate limited), try again later or specify a version: xmray update vx.x.x", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("invalid GitHub API response: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("failed to detect the XMRay version from GitHub API")
	}
	return release.TagName, nil
}

// downloadFile downloads a URL to a local path.
func downloadFile(url, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// unzip extracts a zip archive to a destination directory.
func unzip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		fPath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fPath, os.ModePerm); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fPath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		if _, err := io.Copy(outFile, rc); err != nil {
			outFile.Close()
			rc.Close()
			return err
		}
		outFile.Close()
		rc.Close()
	}
	return nil
}

// writeSystemdService installs the XMRay systemd unit file.
func writeSystemdService() error {
	unit := `[Unit]
Description=XMRay Service
After=network.target nss-lookup.target
Wants=network.target

[Service]
User=root
Group=root
Type=simple
LimitAS=infinity
LimitRSS=infinity
LimitCORE=infinity
LimitNPROC=10000
LimitNOFILE=1048576
WorkingDirectory=/usr/local/XMRay/
ExecStart=/usr/local/XMRay/XMRay --config /etc/XMRay/config.yml
Restart=on-failure
RestartPreventExitStatus=23
RestartSec=5s
Environment="XRAY_VMESS_AEAD_FORCED=false"

[Install]
WantedBy=multi-user.target
`
	return os.WriteFile(serviceUnitFile, []byte(unit), 0644)
}

// installBaseDeps installs the base packages required by XMRay.
func installBaseDeps(release string) error {
	switch release {
	case "centos":
		if err := runCommandWithOutput("yum", "install", "epel-release", "-y"); err != nil {
			return fmt.Errorf("yum install epel-release: %w", err)
		}
		return runCommandWithOutput("yum", "install", "wget", "curl", "unzip", "tar", "crontabs", "socat", "-y")
	default: // debian, ubuntu
		if err := runCommandWithOutput("apt", "update"); err != nil {
			return fmt.Errorf("apt update: %w", err)
		}
		return runCommandWithOutput("apt", "install", "wget", "curl", "unzip", "tar", "cron", "socat", "-y")
	}
}

// copyDir recursively copies the contents of srcDir to dstDir.
func copyDir(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dstPath := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}
		if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
			return fmt.Errorf("failed to write %s: %w", dstPath, err)
		}
		return nil
	})
}

// copyConfigFiles copies default config files from the install dir to /etc/XMRay
// only when they do not already exist.
func copyConfigFiles() error {
	files := []string{"geoip.dat", "geosite.dat", "dns.json", "route.json", "outbound.json", "config.yml"}
	if err := os.MkdirAll(xmrayConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	// Geo data ships with every release; user-editable configs are only created when missing.
	alwaysOverwrite := map[string]bool{"geoip.dat": true, "geosite.dat": true}

	for _, f := range files {
		src := filepath.Join(xmrayInstallDir, f)
		dst := filepath.Join(xmrayConfigDir, f)

		if _, err := os.Stat(dst); err == nil && !alwaysOverwrite[f] {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("failed to copy %s: %w", f, err)
		}
	}
	return nil
}
