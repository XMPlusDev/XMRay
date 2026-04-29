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
	
	"github.com/xmplusdev/xmray/core/instance"
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
	if cfgFile != "" {
		configName := path.Base(cfgFile)
		configFileExt := path.Ext(cfgFile)
		configNameOnly := strings.TrimSuffix(configName, configFileExt)
		configPath := path.Dir(cfgFile)
		config.SetConfigName(configNameOnly)
		config.SetConfigType(strings.TrimPrefix(configFileExt, "."))
		config.AddConfigPath(configPath)
		os.Setenv("XRAY_LOCATION_ASSET", configPath)
		os.Setenv("XRAY_LOCATION_CONFIG", configPath)
	} else {
		config.SetConfigName("config")
		config.SetConfigType("yml")
		config.AddConfigPath(".")
	}
	if err := config.ReadInConfig(); err != nil {
		log.Panicf("Config file error: %s \n", err)
	}
	config.WatchConfig() 
	return config
}

func run() error {
	showVersion()

	restartChan := make(chan bool, 1)
	lastTime := time.Now()

	config := getConfig()

	config.OnConfigChange(func(e fsnotify.Event) {
		if time.Now().After(lastTime.Add(3 * time.Second)) {
			log.Printf("Config file changed: %s", e.Name)
			lastTime = time.Now()
			select {
			case restartChan <- true:
			default:
				// Channel full, restart already pending
			}
		}
	})

	err := runManager(config, restartChan)
	if err != nil {
		if err.Error() == "reload" {
			cmd := exec.Command("xmray", "restart")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				log.Errorf("Failed to execute restart command: %s", err)
				return err
			}

			return nil
		}
		return err
	}

	return nil
}

func runManager(config *viper.Viper, restartChan chan bool) (err error) {
	if config == nil {
		return fmt.Errorf("config is nil")
	}

	instanceConfig := &instance.Config{}
	if err := config.Unmarshal(instanceConfig); err != nil {
		return fmt.Errorf("Parse config file %v failed: %s", cfgFile, err)
	}

	if instanceConfig == nil {
		return fmt.Errorf("instance config is nil after unmarshaling")
	}

	log.SetReportCaller(instanceConfig.LogConfig.Level == "debug")

	i := instance.New(instanceConfig)
	if i == nil {
		return fmt.Errorf("failed to create instance")
	}

	if err := startInstanceSafely(i); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	defer func() {
		if i != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Errorf("Panic during instance close: %v", r)
					}
				}()
				if closeErr := i.Close(); closeErr != nil {
					if err == nil {
						err = fmt.Errorf("stop instance: %w", closeErr)
					}
				}
			}()
		}
	}()

	runtime.GC()

	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(osSignals)

	select {
	case sig := <-osSignals:
		log.Printf("Received signal: %v, shutting down gracefully...", sig)
		return nil
	case <-restartChan:
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

func startInstanceSafely(i *instance.Instance) (err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := formatStack(debug.Stack())
			err = fmt.Errorf("panic during instance start: %v\nStack trace:\n%s", r, stack)
		}
	}()

	if i == nil {
		return fmt.Errorf("instance is nil")
	}

	return i.Start()
}

func Execute() error {
	return rootCmd.Execute()
}