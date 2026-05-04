package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	"dario.cat/mergo"
	"github.com/r3labs/diff/v2"
	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/stats"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/controller"
	limitDispatcher "github.com/xmplusdev/xmray/core/dispatcher"
	"github.com/xmplusdev/xmray/helper/limiter"
	_ "github.com/xmplusdev/xmray/main/distro/all"
)

type Instance struct {
	statusLock     sync.Mutex
	instanceConfig *Config
	Server         *core.Instance
	Dispatcher     *limitDispatcher.LimitingDispatcher
	Service        []controller.ControllerInterface
	Running        bool

	reverbCancels []context.CancelFunc
	controllerMap map[int]controller.TriggerInterface
}

func New(instanceConfig *Config) *Instance {
	return &Instance{instanceConfig: instanceConfig}
}

func (i *Instance) loadCore(instanceConfig *Config) (*core.Instance, error) {
	coreLogConfig := &conf.LogConfig{}
	logConfig := getDefaultLogConfig()
	if instanceConfig.LogConfig != nil {
		if _, err := diff.Merge(logConfig, instanceConfig.LogConfig, logConfig); err != nil {
			return nil, fmt.Errorf("read Log config failed: %s", err)
		}
	}
	coreLogConfig.LogLevel = logConfig.Level
	coreLogConfig.AccessLog = logConfig.AccessPath
	coreLogConfig.ErrorLog = logConfig.ErrorPath
	coreLogConfig.DNSLog = logConfig.DNSLog
	coreLogConfig.MaskAddress = logConfig.MaskAddress

	coreDnsConfig := &conf.DNSConfig{}
	if instanceConfig.DnsConfigPath != "" {
		data, err := os.ReadFile(instanceConfig.DnsConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read DNS config file at: %s", instanceConfig.DnsConfigPath)
		}
		if err = json.Unmarshal(data, coreDnsConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal DNS config: %s", instanceConfig.DnsConfigPath)
		}
	}
	dnsConfig, err := coreDnsConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to understand DNS config: %s", err)
	}

	coreRouterConfig := &conf.RouterConfig{}
	if instanceConfig.RouteConfigPath != "" {
		data, err := os.ReadFile(instanceConfig.RouteConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read Routing config file at: %s", instanceConfig.RouteConfigPath)
		}
		if err = json.Unmarshal(data, coreRouterConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Routing config: %s", instanceConfig.RouteConfigPath)
		}
	}
	routeConfig, err := coreRouterConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to understand Routing config: %s", err)
	}

	var inBoundConfig []*core.InboundHandlerConfig

	var coreCustomOutboundConfig []conf.OutboundDetourConfig
	if instanceConfig.OutboundConfigPath != "" {
		data, err := os.ReadFile(instanceConfig.OutboundConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read Custom Outbound config file at: %s", instanceConfig.OutboundConfigPath)
		}
		if err = json.Unmarshal(data, &coreCustomOutboundConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Custom Outbound config: %s", instanceConfig.OutboundConfigPath)
		}
	}
	var outBoundConfig []*core.OutboundHandlerConfig
	for _, cfg := range coreCustomOutboundConfig {
		oc, err := cfg.Build()
		if err != nil {
			return nil, fmt.Errorf("failed to understand Outbound config: %s", err)
		}
		outBoundConfig = append(outBoundConfig, oc)
	}

	levelPolicyConfig := policyConnectionConfig(instanceConfig.ConnectionConfig)
	corePolicyConfig := &conf.PolicyConfig{}
	corePolicyConfig.Levels = map[uint32]*conf.Policy{0: levelPolicyConfig}
	policyConfig, _ := corePolicyConfig.Build()

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

func (i *Instance) Start() error {
	i.statusLock.Lock()
	defer i.statusLock.Unlock()

	server, err := i.loadCore(i.instanceConfig)
	if err != nil {
		return fmt.Errorf("failed to load config: %s", err)
	}

	lim := limiter.New()
	ld, err := limitDispatcher.RegisterOn(server, lim)
	if err != nil {
		return fmt.Errorf("failed to register limiting dispatcher: %s", err)
	}

	for _, s := range i.Service {
		if err := s.Close(); err != nil {
			return fmt.Errorf("warning: failed to close service during restart: %s", err)
		}
	}
	i.Service = nil

	if i.Server != nil {
		i.Server.Close()
	}
	i.Server = nil
	i.Dispatcher = nil

	for _, cancel := range i.reverbCancels {
		cancel()
	}
	i.reverbCancels = nil
	i.controllerMap = make(map[int]controller.TriggerInterface)

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start instance: %s", err)
	}
	i.Server = server
	i.Dispatcher = ld

	log.Println("XMRay started successfully")

	for _, nodeConfig := range i.instanceConfig.NodesConfig {
		client := api.New(nodeConfig.ApiConfig)

		controllerConfig := getDefaultControllerConfig()
		if nodeConfig.ControllerConfig != nil {
			if err := mergo.Merge(controllerConfig, nodeConfig.ControllerConfig, mergo.WithOverride); err != nil {
				return fmt.Errorf("read Controller Config Failed: %s", err)
			}
		}

		controllerService := controller.New(server, client, controllerConfig, i.Dispatcher)
		i.Service = append(i.Service, controllerService)
	}

	for _, s := range i.Service {
		if err := s.Start(); err != nil {
			return fmt.Errorf("XMRay failed to start: %s", err)
		}

		if t, ok := s.(controller.TriggerInterface); ok {
			nodeID := t.GetNodeID()
			if _, exists := i.controllerMap[nodeID]; !exists {
				i.controllerMap[nodeID] = t
			}
		}
	}

	for _, cfg := range i.instanceConfig.ReverbConfig {
		if cfg == nil || !cfg.Enable {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		i.reverbCancels = append(i.reverbCancels, cancel)
		go i.reverbListener(ctx, cfg)
	}

	i.Running = true
	return nil
}

func (i *Instance) Close() error {
	i.statusLock.Lock()
	defer i.statusLock.Unlock()

	for _, cancel := range i.reverbCancels {
		cancel()
	}
	i.reverbCancels = nil

	for _, s := range i.Service {
		if err := s.Close(); err != nil {
			return fmt.Errorf("warning: failed to close service during restart: %s", err)
		}
	}
	i.Service = nil
	i.controllerMap = nil
	i.Dispatcher = nil
	i.Server.Close()
	i.Running = false
	return nil
}

func policyConnectionConfig(c *ConnectionConfig) (policy *conf.Policy) {
	connectionConfig := getDefaultConnectionConfig()
	if c != nil {
		if _, err := diff.Merge(connectionConfig, c, connectionConfig); err != nil {
			log.Panicf("read ConnectionConfig failed: %s", err)
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
