package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xmplusdev/xmplus-server/manager"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use: "XMPlus",
		Run: func(cmd *cobra.Command, args []string) {
			if err := run(); err != nil {
				log.Fatal(err)
			}
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "Config file for XMPlus.")
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
		// Set ASSET Path and Config Path for XMPlus
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
		if time.Now().After(lastTime.Add(3 * time.Second)) {
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
			
			// Execute terminal command "xmplus restart"
			cmd := exec.Command("xmplus", "restart")
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

func runManager(config *viper.Viper, restartChan chan bool) error {
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
	
	if managerConfig.LogConfig.Level == "debug" {
		log.SetReportCaller(true)
	} else {
		log.SetReportCaller(false)
	}
	
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
			// Recover from potential panic in Close()
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Errorf("Panic during manager close: %v", r)
					}
				}()
				m.Close()
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

// startManagerSafely starts the manager with panic recovery
func startManagerSafely(m *manager.Manager) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during manager start: %v", r)
		}
	}()
	
	if m == nil {
		return fmt.Errorf("manager is nil")
	}
	
	m.Start()
	return nil
}

func Execute() error {
	return rootCmd.Execute()
}