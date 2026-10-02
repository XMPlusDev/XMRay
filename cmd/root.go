package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
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
	return config
}

func run() error {
	showVersion()

	restartChan := make(chan bool, 1)

	config := getConfig()

	// Debounce timestamp shared with viper's watcher goroutine. An atomic
	// (instead of a plain time.Time) keeps the read-modify-write race-free,
	// since OnConfigChange fires from a separate goroutine.
	var lastChange atomic.Int64
	lastChange.Store(time.Now().UnixNano())

	config.OnConfigChange(func(e fsnotify.Event) {
		// viper invokes this on its own goroutine, which has no recover up
		// the stack — a panic here would otherwise crash the whole process.
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("Recovered from panic in config change handler: %v", r)
			}
		}()

		now := time.Now().UnixNano()
		prev := lastChange.Load()
		if now-prev < int64(3*time.Second) {
			return
		}
		if !lastChange.CompareAndSwap(prev, now) {
			return // a concurrent event already claimed this debounce window
		}

		log.Printf("Config file changed: %s", e.Name)
		select {
		case restartChan <- true:
		default:
		}
	})

	// Start watching only after the handler is registered, so an early
	// filesystem event can't race against an unset callback.
	config.WatchConfig()

	err := runManager(config, restartChan)
	if err != nil {
		if err.Error() == "reload" {
			log.Println("Restarting process...")
			exe, execErr := os.Executable()
			if execErr != nil {
				return fmt.Errorf("get executable path: %w", execErr)
			}
			if execErr = syscall.Exec(exe, os.Args, os.Environ()); execErr != nil {
				return fmt.Errorf("re-exec process: %w", execErr)
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

	logLevel := ""
	if instanceConfig.InstanceConfig != nil && instanceConfig.InstanceConfig.LogConfig != nil {
		logLevel = instanceConfig.InstanceConfig.LogConfig.Level
	}
	log.SetReportCaller(logLevel == "debug")

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
