package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xmplusdev/xmray/manager"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use: "XMRay",
		Run: func(cmd *cobra.Command, args []string) {
			if err := run(); err != nil {
				log.Fatal(err)
			}
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "Config file for XMRay.")
}

func getConfig() *viper.Viper {
	config := viper.New()
	// Set custom path and name
	if cfgFile != "" {
		configName := path.Base(cfgFile)
		configFileExt := path.Ext(cfgFile)
		configNameOnly := strings.TrimSuffix(configName, configFileExt)
		configPath := path.Dir(cfgFile)
		config.SetConfigName(configNameOnly)
		config.SetConfigType(strings.TrimPrefix(configFileExt, "."))
		config.AddConfigPath(configPath)
		// Set ASSET Path and Config Path for XMRay
		os.Setenv("XRAY_LOCATION_ASSET", configPath)
		os.Setenv("XRAY_LOCATION_CONFIG", configPath)
	} else {
		// Set default config path
		config.SetConfigName("config")
		config.SetConfigType("yml")
		config.AddConfigPath(".")
	}
	if err := config.ReadInConfig(); err != nil {
		log.Panicf("Config file error: %s \n", err)
	}
	config.WatchConfig() // Watch the config
	return config
}

func run() error {
	showVersion()

	// Channel for triggering restarts
	restartChan := make(chan bool, 1)
	lastTime := time.Now()

	config := getConfig()

	config.OnConfigChange(func(e fsnotify.Event) {
		// Discarding event received within a short period of time after receiving an event.
		if time.Now().After(lastTime.Add(2 * time.Second)) {
			log.Printf("Config file changed: %s", e.Name)
			lastTime = time.Now()
			// Trigger restart
			select {
			case restartChan <- true:
			default:
				// Channel full, restart already pending
			}
		}
	})

	err := runManager(config, restartChan)
	if err != nil {
		// Check if it's a reload signal
		if err.Error() == "reload" {
			// Execute terminal command "xmray restart"
			cmd := exec.Command("xmray", "restart")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				log.Errorf("Failed to execute restart command: %s", err)
				return err
			}

			return nil
		}
		// Fatal error
		return err
	}

	return nil
}

func runManager(config *viper.Viper, restartChan chan bool) (err error) {
	// Validate config is not nil
	if config == nil {
		return fmt.Errorf("config is nil")
	}

	managerConfig := &manager.Config{}
	if err := config.Unmarshal(managerConfig); err != nil {
		return fmt.Errorf("Parse config file %v failed: %s", cfgFile, err)
	}

	// Validate managerConfig before proceeding
	if managerConfig == nil {
		return fmt.Errorf("manager config is nil after unmarshaling")
	}

	log.SetReportCaller(managerConfig.LogConfig.Level == "debug")

	m := manager.New(managerConfig)
	if m == nil {
		return fmt.Errorf("failed to create manager instance")
	}

	// Start manager with error handling
	if err := startManagerSafely(m); err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	defer func() {
		if m != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Errorf("Panic during manager close: %v", r)
					}
				}()
				if closeErr := m.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("stop manager: %w", closeErr)
					}
				}
			}()
		}
	}()

	// Explicitly triggering GC to remove garbage from config loading.
	runtime.GC()

	// Running backend
	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(osSignals)

	select {
	case sig := <-osSignals:
		// Received termination signal, exit completely
		log.Printf("Received signal: %v, shutting down gracefully...", sig)
		return nil
	case <-restartChan:
		// Config changed, return to restart
		return fmt.Errorf("reload")
	}
}

func formatStack(stack []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stack)), "\n")
	var b strings.Builder

	if len(lines) > 0 {
		b.WriteString(lines[0])
		b.WriteByte('\n')
		lines = lines[1:]
	}

	for i := 0; i+1 < len(lines); i += 2 {
		fn := strings.TrimSpace(lines[i])
		loc := strings.TrimSpace(lines[i+1])
		b.WriteString(fmt.Sprintf("  → %s\n      %s\n", fn, loc))
	}

	if len(lines)%2 != 0 {
		b.WriteString("  → ")
		b.WriteString(strings.TrimSpace(lines[len(lines)-1]))
		b.WriteByte('\n')
	}

	return b.String()
}

// startManagerSafely starts the manager with panic recovery
func startManagerSafely(m *manager.Manager) (err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := formatStack(debug.Stack())
			err = fmt.Errorf("panic during instance start: %v\nStack trace:\n%s", r, stack)
		}
	}()

	if m == nil {
		return fmt.Errorf("manager is nil")
	}

	return m.Start()
}

func Execute() error {
	return rootCmd.Execute()
}