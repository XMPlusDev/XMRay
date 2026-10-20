package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall XMRay",
	Long: `Uninstall XMRay from the system.

This stops and disables the service, removes the systemd unit,
the installation directory, and the management command from /usr/bin.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return executeUninstall()
	},
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

func executeUninstall() error {
	if err := checkRoot(); err != nil {
		return err
	}
	if err := checkInstalled(); err != nil {
		return err
	}

	ok, err := confirm("Are you sure you want to uninstall XMRay?", false)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Uninstall cancelled")
		return nil
	}

	if isSystemdAvailable() {
		runCommandWithOutput("systemctl", "stop", serviceName)
		runCommandWithOutput("systemctl", "disable", serviceName)
		os.Remove(serviceUnitFile)
		runCommandWithOutput("systemctl", "daemon-reload")
	} else {
		runCommandWithOutput("rc-service", "xmray", "stop")
		runCommandWithOutput("rc-update", "del", "xmray", "default")
	}

	os.RemoveAll(xmrayInstallDir)
	os.Remove(filepath.Join(xmrayBinDir, xmrayBinName))
	os.Remove(filepath.Join(xmrayBinDir, xmrayLinkName))

	fmt.Println("XMRay uninstalled successfully")
	return nil
}
