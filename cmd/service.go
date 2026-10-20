package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(enableCmd)
	rootCmd.AddCommand(disableCmd)
	rootCmd.AddCommand(logCmd)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start XMRay",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkRoot(); err != nil {
			return err
		}
		if err := checkInstalled(); err != nil {
			return err
		}
		if isSystemdAvailable() {
			return runCommandWithOutput("systemctl", "start", serviceName)
		}
		return runCommandWithOutput("rc-service", "xmray", "start")
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop XMRay",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkRoot(); err != nil {
			return err
		}
		if err := checkInstalled(); err != nil {
			return err
		}
		if isSystemdAvailable() {
			return runCommandWithOutput("systemctl", "stop", serviceName)
		}
		return runCommandWithOutput("rc-service", "xmray", "stop")
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart XMRay",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkRoot(); err != nil {
			return err
		}
		if err := checkInstalled(); err != nil {
			return err
		}
		if isSystemdAvailable() {
			return runCommandWithOutput("systemctl", "restart", serviceName)
		}
		if err := runCommandWithOutput("rc-service", "xmray", "restart"); err != nil {
			return runCommandWithOutput("rc-service", "xmray", "start")
		}
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "View XMRay status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkInstalled(); err != nil {
			return err
		}
		if !isSystemdAvailable() {
			return fmt.Errorf("systemd is not available on this system")
		}
		out, err := runCommand("systemctl", "status", serviceName)
		if err != nil {
			// service may be inactive; still print the status output
			fmt.Print(string(out))
			return nil
		}
		fmt.Print(string(out))
		return nil
	},
}

var enableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable XMRay auto-start",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkRoot(); err != nil {
			return err
		}
		if err := checkInstalled(); err != nil {
			return err
		}
		if isSystemdAvailable() {
			return runCommandWithOutput("systemctl", "enable", serviceName)
		}
		return runCommandWithOutput("rc-update", "add", "xmray", "default")
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable XMRay auto-start",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkRoot(); err != nil {
			return err
		}
		if err := checkInstalled(); err != nil {
			return err
		}
		if isSystemdAvailable() {
			return runCommandWithOutput("systemctl", "disable", serviceName)
		}
		return runCommandWithOutput("rc-update", "del", "xmray", "default")
	},
}

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "View XMRay logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkInstalled(); err != nil {
			return err
		}
		if isSystemdAvailable() {
			cmd := exec.Command("journalctl", "-u", serviceName, "--no-pager")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
		return fmt.Errorf("systemd is not available on this system")
	},
}


