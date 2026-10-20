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
			if osVersion != "" && mustBeLE(osVersion, 6) {
				return fmt.Errorf("please use CentOS 7 or later")
			}
		case "ubuntu":
			if osVersion != "" && mustBeLT(osVersion, 16) {
				return fmt.Errorf("please use Ubuntu 16 or later system")
			}
		case "debian":
			if osVersion != "" && mustBeLT(osVersion, 8) {
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

	os.RemoveAll(xmrayInstallDir)
	os.Remove(filepath.Join(xmrayBinDir, xmrayBinName))
	os.Remove(filepath.Join(xmrayBinDir, xmrayLinkName))

	if err := os.MkdirAll(xmrayInstallDir, 0755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}


	zipURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/XMRay-linux-%s.zip", xmrayRepo, version, arch)
	zipPath := filepath.Join(xmrayInstallDir, "XMRay-linux.zip")

	fmt.Printf("Downloading %s ...\n", zipURL)
	if err := downloadFile(zipURL, zipPath); err != nil {
		return fmt.Errorf("failed to download XMRay, please make sure your server can access GitHub: %w", err)
	}

	fmt.Println("Extracting...")
	if err := unzip(zipPath, xmrayInstallDir); err != nil {
		return fmt.Errorf("failed to extract XMRay: %w", err)
	}
	os.Remove(zipPath)

	binSrc := filepath.Join(xmrayInstallDir, xmrayBinName)
	if _, err := os.Stat(binSrc); err != nil {
		return fmt.Errorf("XMRay binary not found in release package: %w", err)
	}

	if isSystemdAvailable() {
		if err := writeSystemdService(); err != nil {
			return fmt.Errorf("failed to write systemd service: %w", err)
		}
		if err := runCommandWithOutput("systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}
		if err := runCommandWithOutput("systemctl", "stop", serviceName); err != nil {
		}
		if err := runCommandWithOutput("systemctl", "enable", serviceName); err != nil {
			return fmt.Errorf("systemctl enable: %w", err)
		}
	}

	if err := copyConfigFiles(); err != nil {
		return err
	}

	binDst := filepath.Join(xmrayBinDir, xmrayBinName)
	if data, err := os.ReadFile(binSrc); err == nil {
		if err := os.WriteFile(binDst, data, 0755); err != nil {
			return fmt.Errorf("failed to install binary: %w", err)
		}
	} else {
		return fmt.Errorf("failed to read XMRay binary: %w", err)
	}

	linkDst := filepath.Join(xmrayBinDir, xmrayLinkName)
	os.Remove(linkDst)
	if err := os.Symlink(binDst, linkDst); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	if isSystemdAvailable() {
		if err := runCommandWithOutput("systemctl", "restart", serviceName); err != nil {
			return fmt.Errorf("failed to start XMRay, use [ xmray log ] to view logs: %w", err)
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