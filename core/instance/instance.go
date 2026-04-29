package instance

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	"dario.cat/mergo"
	"github.com/r3labs/diff/v2"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/stats"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/app/dispatcher"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/controller"
	"github.com/xmplusdev/xmray/helper/limiter"
	limitDispatcher "github.com/xmplusdev/xmray/core/dispatcher"
	_ "github.com/xmplusdev/xmray/main/distro/all"
)

type Instance struct {
	statusLock    sync.Mutex
	instanceConfig *Config
	Server        *core.Instance
	Dispatcher    *limitDispatcher.LimitingDispatcher
	Service       []controller.ControllerInterface
	Running       bool
}

func New(instanceConfig *Config) *Instance {
	i := &Instance{instanceConfig: instanceConfig}
	return i
}

func (i *Instance) loadCore(instanceConfig *Config) (*core.Instance, error) {
	// Log Config
	coreLogConfig := &conf.LogConfig{}
	logConfig := getDefaultLogConfig()
	if instanceConfig.LogConfig != nil {
		if _, err := diff.Merge(logConfig, instanceConfig.LogConfig, logConfig); err != nil {
			return nil, fmt.Errorf("Read Log config failed: %s", err)
		}
	}
	coreLogConfig.LogLevel = logConfig.Level
	coreLogConfig.AccessLog = logConfig.AccessPath
	coreLogConfig.ErrorLog = logConfig.ErrorPath
	coreLogConfig.DNSLog = logConfig.DNSLog
	coreLogConfig.MaskAddress = logConfig.MaskAddress

	// DNS config
	coreDnsConfig := &conf.DNSConfig{}
	if instanceConfig.DnsConfigPath != "" {
		data, err := os.ReadFile(instanceConfig.DnsConfigPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read DNS config file at: %s", instanceConfig.DnsConfigPath)
		}
		if err = json.Unmarshal(data, coreDnsConfig); err != nil {
			return nil, fmt.Errorf("Failed to unmarshal DNS config: %s", instanceConfig.DnsConfigPath)
		}
	}
	dnsConfig, err := coreDnsConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("Failed to understand DNS config, Please check: https://xtls.github.io/config/dns.html for help: %s", err)
	}

	// Routing config
	coreRouterConfig := &conf.RouterConfig{}
	if instanceConfig.RouteConfigPath != "" {
		data, err := os.ReadFile(instanceConfig.RouteConfigPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read Routing config file at: %s", instanceConfig.RouteConfigPath)
		}
		if err = json.Unmarshal(data, coreRouterConfig); err != nil {
			return nil, fmt.Errorf("Failed to unmarshal Routing config: %s", instanceConfig.RouteConfigPath)
		}
	}
	routeConfig, err := coreRouterConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("Failed to understand Routing config, Please check: https://xtls.github.io/config/routing.html for help: %s", err)
	}

	var inBoundConfig []*core.InboundHandlerConfig

	// Custom Outbound config
	var coreCustomOutboundConfig []conf.OutboundDetourConfig
	if instanceConfig.OutboundConfigPath != "" {
		data, err := os.ReadFile(instanceConfig.OutboundConfigPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read Custom Outbound config file at: %s", instanceConfig.OutboundConfigPath)
		}
		if err = json.Unmarshal(data, &coreCustomOutboundConfig); err != nil {
			return nil, fmt.Errorf("Failed to unmarshal Custom Outbound config: %s", instanceConfig.OutboundConfigPath)
		}
	}
	var outBoundConfig []*core.OutboundHandlerConfig
	for _, config := range coreCustomOutboundConfig {
		oc, err := config.Build()
		if err != nil {
			return nil, fmt.Errorf("Failed to understand Outbound config, Please check: https://xtls.github.io/config/outbound.html for help: %s", err)
		}
		outBoundConfig = append(outBoundConfig, oc)
	}

	// Policy config
	levelPolicyConfig := parseConnectionConfig(instanceConfig.ConnectionConfig)
	corePolicyConfig := &conf.PolicyConfig{}
	corePolicyConfig.Levels = map[uint32]*conf.Policy{0: levelPolicyConfig}
	policyConfig, _ := corePolicyConfig.Build()

	// Build Core Config
	config := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(coreLogConfig.Build()),
			serial.ToTypedMessage(&dispatcher.Config{}), 
			serial.ToTypedMessage(&stats.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
			serial.ToTypedMessage(policyConfig),
			serial.ToTypedMessage(dnsConfig),
			serial.ToTypedMessage(routeConfig),
		},
		Inbound:  inBoundConfig,
		Outbound: outBoundConfig,
	}

	server, err := core.New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %s", err)
	}

	return server, nil
}

// Start the manager
func (i *Instance) Start() error {
	i.statusLock.Lock()
	defer i.statusLock.Unlock()
	
	server, err := i.loadCore(i.instanceConfig)
	if err != nil {
		return fmt.Errorf("Failed to load config: %s", err)
	}
	
	lim := limiter.New()
	ld, err := limitDispatcher.RegisterOn(server, lim)
	if err != nil {
		return fmt.Errorf("Failed to register limiting dispatcher: %s", err)
	}

	for _, s := range i.Service {
		if err := s.Close(); err != nil {
			return fmt.Errorf("Warning: Failed to close service during restart: %s", err)
		}
	}
	i.Service = nil

	if i.Server != nil {
		i.Server.Close()
	}
	i.Server = nil
	
	if i.Dispatcher != nil {
		i.Dispatcher = nil
	}

	if err := server.Start(); err != nil {
		return fmt.Errorf("Failed to start instance: %s", err)
	}
	i.Server = server
	i.Dispatcher = ld

	log.Println("XMRay started successfully")

	for _, nodeConfig := range i.instanceConfig.NodesConfig {
		var client api.API
		client = api.New(nodeConfig.ApiConfig)

		controllerConfig := getDefaultControllerConfig()
		if nodeConfig.ControllerConfig != nil {
			if err := mergo.Merge(controllerConfig, nodeConfig.ControllerConfig, mergo.WithOverride); err != nil {
				return fmt.Errorf("Read Controller Config Failed: %s", err)
			}
		}
		controllerService := controller.New(server, client, controllerConfig, i.Dispatcher)
		i.Service = append(i.Service, controllerService)
	}

	for _, s := range i.Service {
		if err := s.Start(); err != nil {
			return fmt.Errorf("XMRay failed to start: %s", err)
		}
	}
	i.Running = true
	return nil
}

func (i *Instance) Close() error {
	i.statusLock.Lock()
	defer i.statusLock.Unlock()

	for _, s := range i.Service {
		if err := s.Close(); err != nil {
			return fmt.Errorf("Warning: Failed to close service during restart: %s", err)
		}
	}
	i.Service = nil
	i.Dispatcher = nil
	i.Server.Close()
	i.Running = false
	return nil
}

func parseConnectionConfig(c *ConnectionConfig) (policy *conf.Policy) {
	connectionConfig := getDefaultConnectionConfig()
	if c != nil {
		if _, err := diff.Merge(connectionConfig, c, connectionConfig); err != nil {
			log.Panicf("Read ConnectionConfig failed: %s", err)
		}
	}
	policy = &conf.Policy{
		StatsUserUplink:   true,
		StatsUserDownlink: true,
		Handshake:         &connectionConfig.Handshake,
		ConnectionIdle:    &connectionConfig.ConnIdle,
		UplinkOnly:        &connectionConfig.UplinkOnly,
		DownlinkOnly:      &connectionConfig.DownlinkOnly,
		BufferSize:        &connectionConfig.BufferSize,
	}
	return
}
