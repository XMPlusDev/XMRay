package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

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
	"github.com/xmplusdev/xmray/limiter"
	"github.com/xmplusdev/xmray/helper/scheduler"
	_ "github.com/xmplusdev/xmray/core/distro/all"
)

type Instance struct {
	statusLock     sync.Mutex
	instanceConfig *Config
	Server         *core.Instance
	Dispatcher     *limitDispatcher.LimitingDispatcher
	Limiter        *limiter.Limiter
	Service        []controller.ControllerInterface
	Running        bool

	reverbCancels    []context.CancelFunc
	controllerMap    map[int]controller.TriggerInterface
	reverbOutbound   chan reverbOutbound
	reverbMu         sync.Mutex
	currentPusher    func(string, any) error
	serverPoller      *scheduler.PeriodicTask
	serverPollTrigger chan struct{}
	serverStatusTask  *scheduler.PeriodicTask
}

func New(instanceConfig *Config) *Instance {
	return &Instance{
		instanceConfig:    instanceConfig,
		reverbOutbound:    make(chan reverbOutbound, 64),
		serverPollTrigger: make(chan struct{}, 1),
	}
}

func (i *Instance) PushEvent(event string, data any) error {
	result := make(chan error, 1)
	i.reverbOutbound <- reverbOutbound{event: event, data: data, result: result}
	return <-result
}

func (i *Instance) drainReverbOutbound() {
	for ob := range i.reverbOutbound {
		i.reverbMu.Lock()
		pusher := i.currentPusher
		i.reverbMu.Unlock()

		if pusher != nil {
			ob.result <- pusher(ob.event, ob.data)
		} else {
			ob.result <- fmt.Errorf("no active Reverb connection")
		}
	}
}

func (i *Instance) loadCore(instanceConfig *Config) (*core.Instance, error) {
	ic := instanceConfig.InstanceConfig
	if ic == nil {
		ic = &InstanceConfig{}
	}

	coreLogConfig := &conf.LogConfig{}
	logConfig := getDefaultLogConfig()
	if ic.LogConfig != nil {
		if _, err := diff.Merge(logConfig, ic.LogConfig, logConfig); err != nil {
			return nil, fmt.Errorf("read Log config failed: %s", err)
		}
	}
	coreLogConfig.LogLevel = logConfig.Level
	coreLogConfig.AccessLog = logConfig.AccessPath
	coreLogConfig.ErrorLog = logConfig.ErrorPath
	coreLogConfig.DNSLog = logConfig.DNSLog
	coreLogConfig.MaskAddress = logConfig.MaskAddress

	coreDnsConfig := &conf.DNSConfig{}
	if ic.DNSConfig != nil {
		if ic.DNSConfig.Enable && ic.DNSConfig.Path != "" {
			data, err := os.ReadFile(ic.DNSConfig.Path)
			if err != nil {
				return nil, fmt.Errorf("failed to read DNS config file at: %s", ic.DNSConfig.Path)
			}
			if err = json.Unmarshal(data, coreDnsConfig); err != nil {
				return nil, fmt.Errorf("failed to unmarshal DNS config: %s", ic.DNSConfig.Path)
			}
		}
		if ic.DNSConfig.Strategy != "" {
			coreDnsConfig.QueryStrategy = ic.DNSConfig.Strategy
		}
	}
	dnsConfig, err := coreDnsConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to understand DNS config: %s", err)
	}

	coreRouterConfig := &conf.RouterConfig{}
	if ic.RouteConfig != nil && ic.RouteConfig.Enable && ic.RouteConfig.Path != "" {
		data, err := os.ReadFile(ic.RouteConfig.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read Routing config file at: %s", ic.RouteConfig.Path)
		}
		if err = json.Unmarshal(data, coreRouterConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Routing config: %s", ic.RouteConfig.Path)
		}
	}
	routeConfig, err := coreRouterConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to understand Routing config: %s", err)
	}

	var inBoundConfig []*core.InboundHandlerConfig

	var coreCustomOutboundConfig []conf.OutboundDetourConfig
	if ic.OutboundConfig != nil && ic.OutboundConfig.Enable && ic.OutboundConfig.Path != "" {
		data, err := os.ReadFile(ic.OutboundConfig.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read Custom Outbound config file at: %s", ic.OutboundConfig.Path)
		}
		if err = json.Unmarshal(data, &coreCustomOutboundConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Custom Outbound config: %s", ic.OutboundConfig.Path)
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

	levelPolicyConfig := policyConnectionConfig(ic.ConnectionConfig)
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

	lim := limiter.New(server, i.instanceConfig.RedisConfig)
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
	if i.serverPoller != nil {
		i.serverPoller.Close()
		i.serverPoller = nil
	}
	if i.serverStatusTask != nil {
		i.serverStatusTask.Close()
		i.serverStatusTask = nil
	}
	i.controllerMap = make(map[int]controller.TriggerInterface)

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start instance: %s", err)
	}
	i.Server = server
	i.Dispatcher = ld
	i.Limiter = lim

	log.Println("XMRay started successfully")

	pusher := i.PushEvent

	log.Printf("[Reverb] config: %d entries: %+v", len(i.instanceConfig.ReverbConfig), i.instanceConfig.ReverbConfig)
	for _, cfg := range i.instanceConfig.ReverbConfig {
		if cfg == nil || !cfg.Enable {
			log.Printf("[Reverb] skipping entry: %+v", cfg)
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		i.reverbCancels = append(i.reverbCancels, cancel)
		go i.reverbListener(ctx, cfg)
	}

	go i.drainReverbOutbound()

	if i.instanceConfig.ApiConfig != nil && i.instanceConfig.ApiConfig.ServerID > 0 {
		rootClient := api.New(i.instanceConfig.ApiConfig)
		controllerConfig := buildControllerConfig(i.instanceConfig)
		controllerConfig.DisableServerMonitor = true

		resp, err := rootClient.GetServerNodes()
		if err != nil {
			return fmt.Errorf("failed to fetch server nodes: %s", err)
		}
	
		if resp.Version < 2606120 {
			return fmt.Errorf("Backend does not support panel api version before v2606120. Update your panel.  Your panel api version is: %d", resp.Version)
		}

		for _, n := range resp.Nodes {
			nodeClient := rootClient.ForNode(n.NodeID)
			svc := controller.New(server, nodeClient, controllerConfig, i.Dispatcher, pusher)
			i.Service = append(i.Service, svc)
		}

		for _, s := range i.Service {
			if err := s.Start(); err != nil {
				log.Printf("XMRay node failed to start: %s", err)
				continue
			}
			if t, ok := s.(controller.TriggerInterface); ok {
				nodeID := t.GetNodeID()
				if _, exists := i.controllerMap[nodeID]; !exists {
					i.controllerMap[nodeID] = t
				}
			}
		}

		i.startServerNodePoller(rootClient, controllerConfig, pusher, resp.PollInterval)

		i.startServerStatusTask(rootClient)

	} else {
		return fmt.Errorf("ApiConfig.ServerID is required — XMRay only supports server mode")
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

	if i.serverPoller != nil {
		i.serverPoller.Close()
		i.serverPoller = nil
	}
	if i.serverStatusTask != nil {
		i.serverStatusTask.Close()
		i.serverStatusTask = nil
	}

	for _, s := range i.Service {
		if err := s.Close(); err != nil {
			return fmt.Errorf("warning: failed to close service during restart: %s", err)
		}
	}
	i.Service = nil
	i.controllerMap = nil
	i.Dispatcher = nil
	if i.Limiter != nil {
		i.Limiter.Close()
		i.Limiter = nil
	}
	i.Server.Close()
	i.Running = false
	return nil
}

func policyConnectionConfig(c *ConnectionConfig) *conf.Policy {
	connectionConfig := getDefaultConnectionConfig()
	if c != nil {
		if _, err := diff.Merge(connectionConfig, c, connectionConfig); err != nil {
			log.Panicf("read ConnectionConfig failed: %s", err)
		}
	}
	return &conf.Policy{
		StatsUserUplink:   true,
		StatsUserDownlink: true,
		Handshake:         &connectionConfig.Handshake,
		ConnectionIdle:    &connectionConfig.ConnIdle,
		UplinkOnly:        &connectionConfig.UplinkOnly,
		DownlinkOnly:      &connectionConfig.DownlinkOnly,
		BufferSize:        &connectionConfig.BufferSize,
	}
}
