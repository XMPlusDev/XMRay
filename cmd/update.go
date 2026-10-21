package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [version]",
	Short: "Update XMRay",
	Long: `Update XMRay to the latest release or a specific version.

This command reinstalls XMRay from the GitHub release package,
preserving existing configuration files.

Examples:
  xmray update             # Update to the latest version
  xmray update v1.0.0      # Update to a specific version`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return executeUpdate(args)
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

var installVersion string

func executeUpdate(args []string) error {
	if err := checkRoot(); err != nil {
		return err
	}

	release, err := detectOS()
	if err != nil {
		return err
	}
	fmt.Printf("Detected OS: %s\n", release)

	if osVersion := detectOSVersion(); osVersion != "" {
		fmt.Printf("OS version: %s\n", osVersion)
		switch release {
		case "centos":
			if mustBeLE(osVersion, 6) {
				return fmt.Errorf("please use CentOS 7 or later")
			}
		case "ubuntu":
			if mustBeLT(osVersion, 16) {
				return fmt.Errorf("please use Ubuntu 16 or later system")
			}
		case "debian":
			if mustBeLT(osVersion, 8) {
				return fmt.Errorf("please use Debian 8 or higher")
			}
		}
	}

	arch := detectArch()
	fmt.Printf("Arch: %s\n", arch)

	fmt.Println("Installing base dependencies...")
	if err := installBaseDeps(release); err != nil {
		return fmt.Errorf("failed to install base dependencies: %w", err)
	}

	version := installVersion
	if version == "" && len(args) > 0 {
		version = args[0]
	}
	if version == "" {
		v, err := fetchLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to detect the XMRay version: %w", err)
		}
		version = v
		fmt.Printf("Latest version detected: %s\n", version)
	}

	// Download and extract into a staging directory first, so a failed download
	// never removes the working installation.
	staging := filepath.Join(filepath.Dir(xmrayInstallDir), ".XMRay-update")
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0755); err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	zipURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/XMRay-linux-%s.zip", xmrayRepo, version, arch)
	zipPath := filepath.Join(staging, "XMRay-linux.zip")

	fmt.Printf("Downloading %s ...\n", zipURL)
	if err := downloadFile(zipURL, zipPath); err != nil {
		return fmt.Errorf("failed to download XMRay (make sure the version exists and your server can reach GitHub): %w", err)
	}

	fmt.Println("Extracting...")
	if err := unzip(zipPath, staging); err != nil {
		return fmt.Errorf("failed to extract XMRay: %w", err)
	}
	os.Remove(zipPath)

	if _, err := os.Stat(filepath.Join(staging, xmrayBinName)); err != nil {
		return fmt.Errorf("XMRay binary not found in release package: %w", err)
	}
	if err := os.Chmod(filepath.Join(staging, xmrayBinName), 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// The new package is good: stop the service and swap the install directory.
	if isSystemdAvailable() {
		_ = runCommandWithOutput("systemctl", "stop", serviceName)
	}
	if err := os.RemoveAll(xmrayInstallDir); err != nil {
		return fmt.Errorf("failed to remove the old installation: %w", err)
	}
	if err := os.Rename(staging, xmrayInstallDir); err != nil {
		return fmt.Errorf("failed to move the new installation into place: %w", err)
	}

	if isSystemdAvailable() {
		if err := writeSystemdService(); err != nil {
			return fmt.Errorf("failed to write systemd service: %w", err)
		}
		if err := runCommandWithOutput("systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}
		if err := runCommandWithOutput("systemctl", "enable", serviceName); err != nil {
			return fmt.Errorf("systemctl enable: %w", err)
		}
	}

	// install.sh only starts the service when a config already exists.
	hadConfig := false
	if _, err := os.Stat("/etc/XMRay/config.yml"); err == nil {
		hadConfig = true
	}

	if err := copyConfigFiles(); err != nil {
		return err
	}

	// Like install.sh: the binary stays in the install directory, /usr/bin/xmray links to it.
	binInstalled := filepath.Join(xmrayInstallDir, xmrayBinName)
	os.Remove(filepath.Join(xmrayBinDir, xmrayBinName))
	linkDst := filepath.Join(xmrayBinDir, xmrayLinkName)
	os.Remove(linkDst)
	if err := os.Symlink(binInstalled, linkDst); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	if isSystemdAvailable() {
		if hadConfig {
			if err := runCommandWithOutput("systemctl", "restart", serviceName); err != nil {
				return fmt.Errorf("failed to start XMRay, use [ xmray log ] to view logs: %w", err)
			}
		} else {
			fmt.Println("No /etc/XMRay/config.yml found: edit the default config, then run [ xmray start ].")
		}
	}

	fmt.Printf("\nXMRay %s updated successfully\n", version)
	printUsage()
	return nil
}

func printUsage() {
	fmt.Println("\nXMRay Management usage method:")
	fmt.Println("------------------------------------------")
	fmt.Println("xmray                    - Run XMRay (foreground)")
	fmt.Println("xmray start              - Start XMRay")
	fmt.Println("xmray stop               - Stop XMRay")
	fmt.Println("xmray restart            - Restart XMRay")
	fmt.Println("xmray status             - View XMRay status")
	fmt.Println("xmray enable             - Enable XMRay auto-start")
	fmt.Println("xmray disable            - Disable XMRay auto-start")
	fmt.Println("xmray log                - View XMRay logs")
	fmt.Println("xmray update             - Update XMRay")
	fmt.Println("xmray update vx.x.x      - Update XMRay to a specific version")
	fmt.Println("xmray install            - Install XMRay")
	fmt.Println("xmray uninstall          - Uninstall XMRay")
	fmt.Println("xmray version            - View XMRay version")
	fmt.Println("xmray warp               - Generate cloudflare warp account")
	fmt.Println("xmray x25519             - Generate key pair for X25519 key exchange")
	fmt.Println("xmray mldsa65            - Generate key pair for ML-DSA-65 post-quantum signature")
	fmt.Println("xmray mlkem768           - Generate key pair for ML-KEM-768 post-quantum key exchange")
	fmt.Println("xmray vlessenc           - Generate decryption/encryption json pair (VLESS Encryption)")
	fmt.Println("xmray ping               - Ping a domain with TLS handshake")
	fmt.Println("xmray cert new           - Generate SSL/TLS certificate for domain name")
	fmt.Println("xmray cert renew         - Renew SSL/TLS certificate for domain name")
	fmt.Println("xmray ech                - Generate ECH keys")
	fmt.Println("xmray hash               - Calculate hash for specific certificate")
	fmt.Println("xmray generate           - Generate self-signed TLS certificates")
	fmt.Println("------------------------------------------")
}

func mustBeLE(version string, limit int) bool {
	return parseMajor(version) <= limit
}

func mustBeLT(version string, limit int) bool {
	return parseMajor(version) < limit
}

func parseMajor(version string) int {
	var n int
	for _, c := range version {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}