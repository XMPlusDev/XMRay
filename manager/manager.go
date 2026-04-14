package manager

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

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/controller"
	_ "github.com/xmplusdev/xmray/main/distro/all"
	"github.com/xmplusdev/xmray/app/dispatcher"
)

// Manager Structure
type Manager struct {
	statusLock    sync.Mutex
	managerConfig *Config
	Server        *core.Instance
	Service       []controller.ControllerInterface
	Running       bool
}

// ManagerInterface for dependency injection
type ManagerInterface interface {
	Restart() error
}

func New(managerConfig *Config) *Manager {
	m := &Manager{managerConfig: managerConfig}
	return m
}

func (m *Manager) loadCore(managerConfig *Config) (*core.Instance, error) {
	// Log Config
	coreLogConfig := &conf.LogConfig{}
	logConfig := getDefaultLogConfig()
	if managerConfig.LogConfig != nil {
		if _, err := diff.Merge(logConfig, managerConfig.LogConfig, logConfig); err != nil {
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
	if managerConfig.DnsConfigPath != "" {
		data, err := os.ReadFile(managerConfig.DnsConfigPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read DNS config file at: %s", managerConfig.DnsConfigPath)
		}
		if err = json.Unmarshal(data, coreDnsConfig); err != nil {
			return nil, fmt.Errorf("Failed to unmarshal DNS config: %s", managerConfig.DnsConfigPath)
		}
	}

	dnsConfig, err := coreDnsConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("Failed to understand DNS config, Please check: https://xtls.github.io/config/dns.html for help: %s", err)
	}

	// Routing config
	coreRouterConfig := &conf.RouterConfig{}
	if managerConfig.RouteConfigPath != "" {
		data, err := os.ReadFile(managerConfig.RouteConfigPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read Routing config file at: %s", managerConfig.RouteConfigPath)
		}
		if err = json.Unmarshal(data, coreRouterConfig); err != nil {
			return nil, fmt.Errorf("Failed to unmarshal Routing config: %s", managerConfig.RouteConfigPath)
		}
	}
	routeConfig, err := coreRouterConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("Failed to understand Routing config, Please check: https://xtls.github.io/config/routing.html for help: %s", err)
	}

	// Custom Inbound config
	var coreCustomInboundConfig []conf.InboundDetourConfig
	if managerConfig.InboundConfigPath != "" {
		data, err := os.ReadFile(managerConfig.InboundConfigPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read Custom Inbound config file at: %s", managerConfig.InboundConfigPath)
		}
		if err = json.Unmarshal(data, &coreCustomInboundConfig); err != nil {
			return nil, fmt.Errorf("Failed to unmarshal Custom Inbound config: %s", managerConfig.InboundConfigPath)
		}
	}
	var inBoundConfig []*core.InboundHandlerConfig
	for _, config := range coreCustomInboundConfig {
		oc, err := config.Build()
		if err != nil {
			return nil, fmt.Errorf("Failed to understand Inbound config, Please check: https://xtls.github.io/config/inbound.html for help: %s", err)
		}
		inBoundConfig = append(inBoundConfig, oc)
	}

	// Custom Outbound config
	var coreCustomOutboundConfig []conf.OutboundDetourConfig
	if managerConfig.OutboundConfigPath != "" {
		data, err := os.ReadFile(managerConfig.OutboundConfigPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read Custom Outbound config file at: %s", managerConfig.OutboundConfigPath)
		}
		if err = json.Unmarshal(data, &coreCustomOutboundConfig); err != nil {
			return nil, fmt.Errorf("Failed to unmarshal Custom Outbound config: %s", managerConfig.OutboundConfigPath)
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
	levelPolicyConfig := parseConnectionConfig(managerConfig.ConnectionConfig)
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
func (m *Manager) Start() error {
	m.statusLock.Lock()
	defer m.statusLock.Unlock()

	if m.Server != nil {
		m.Server.Close()
	}
	m.Server = nil

	server, err := m.loadCore(m.managerConfig)
	if err != nil {
		return fmt.Errorf("Failed to load config: %s", err)
	}
	if err := server.Start(); err != nil {
		return fmt.Errorf("Failed to start instance: %s", err)
	}
	m.Server = server

	for _, s := range m.Service {
		if err := s.Close(); err != nil {
			return fmt.Errorf("Warning: Failed to close service during restart: %s", err)
		}
	}
	m.Service = nil

	// Load Nodes config
	for _, nodeConfig := range m.managerConfig.NodesConfig {
		var client api.API
		client = api.New(nodeConfig.ApiConfig)

		var controllerService controller.ControllerInterface
		// Register controller service
		controllerConfig := getDefaultControllerConfig()
		if nodeConfig.ControllerConfig != nil {
			if err := mergo.Merge(controllerConfig, nodeConfig.ControllerConfig, mergo.WithOverride); err != nil {
				return fmt.Errorf("Read Controller Config Failed: %s", err)
			}
		}
		controllerService = controller.New(server, client, controllerConfig)

		// Set manager reference if controller supports it
		if ctrl, ok := controllerService.(interface{ SetManager(ManagerInterface) }); ok {
			ctrl.SetManager(m)
		}

		m.Service = append(m.Service, controllerService)
	}

	// Start all the service
	for _, s := range m.Service {
		if err := s.Start(); err != nil {
			return fmt.Errorf("XMPlus failed to start: %s", err)
		}
	}
	m.Running = true
	return nil
}

// Close the manager
func (m *Manager) Close() error {
	m.statusLock.Lock()
	defer m.statusLock.Unlock()

	for _, s := range m.Service {
		if err := s.Close(); err != nil {
			return fmt.Errorf("Warning: Failed to close service during restart: %s", err)
		}
	}

	m.Service = nil
	m.Server.Close()
	m.Running = false
	return nil
}

// Restart the manager
func (m *Manager) Restart() error {
	m.statusLock.Lock()
	defer m.statusLock.Unlock()

	// Close all services
	for _, s := range m.Service {
		if err := s.Close(); err != nil {
			return fmt.Errorf("Warning: Failed to close service during restart: %s", err)
		}
	}

	// Close the server
	if m.Server != nil {
		m.Server.Close()
	}

	// Clear services
	m.Service = nil
	m.Running = false

	// Reload and start the core
	server, err := m.loadCore(m.managerConfig)
	if err != nil {
		return err
	}
	if err := server.Start(); err != nil {
		return fmt.Errorf("Failed to restart instance: %s", err)
	}
	m.Server = server

	// Reload and start services
	for _, nodeConfig := range m.managerConfig.NodesConfig {
		var apiClient api.API
		apiClient = api.New(nodeConfig.ApiConfig)

		var controllerService controller.ControllerInterface
		// Register controller service
		controllerConfig := getDefaultControllerConfig()
		if nodeConfig.ControllerConfig != nil {
			if err := mergo.Merge(controllerConfig, nodeConfig.ControllerConfig, mergo.WithOverride); err != nil {
				return err
			}
		}
		controllerService = controller.New(server, apiClient, controllerConfig)
		// Set manager reference so controllers can trigger restarts
		if ctrl, ok := controllerService.(interface{ SetManager(ManagerInterface) }); ok {
			ctrl.SetManager(m)
		}
		m.Service = append(m.Service, controllerService)
	}

	// Start all services
	for _, s := range m.Service {
		if err := s.Start(); err != nil {
			return fmt.Errorf("Failed to start service: %s", err)
		}
	}

	m.Running = true
	log.Println("XMPlus restarted successfully")
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