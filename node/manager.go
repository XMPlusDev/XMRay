package node

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	C "github.com/sagernet/sing/common"
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/infra/conf"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/core/dispatcher"
)

type relayRule struct {
	tag string
	cfg *router.Config
}

type relayRuleSet struct {
	mainTag string
	subs    []api.SubscriptionInfo 
	tags    []string               
}

type Manager struct {
	server     *core.Instance
	ibm        inbound.Manager
	obm        outbound.Manager
	router     *router.Router
	dispatcher *dispatcher.LimitingDispatcher
	client     api.API
	blockMu   sync.Mutex
	blockTags map[string][]string
	routeMu    sync.Mutex             
	relayRules map[string][]relayRule 
	routeGen int
	relays   map[string]*relayRuleSet // by relayTag
}

func NewManager(server *core.Instance, d *dispatcher.LimitingDispatcher, client api.API) *Manager {
	return &Manager{
		server:     server,
		ibm:        server.GetFeature(inbound.ManagerType()).(inbound.Manager),
		obm:        server.GetFeature(outbound.ManagerType()).(outbound.Manager),
		router:     server.GetFeature(routing.RouterType()).(*router.Router),
		dispatcher: d,
		client:     client,
		blockTags: make(map[string][]string),
		relayRules: make(map[string][]relayRule),
		relays: make(map[string]*relayRuleSet),
	}
}

func (m *Manager) AddInboundLimiter(tag string, expiry int, nodeSpeedLimit uint64, ignoreIPs []string, subscriptionList *[]api.SubscriptionInfo) error {
	return m.dispatcher.AddInboundLimiter(tag, expiry, nodeSpeedLimit, ignoreIPs, subscriptionList)
}

func (m *Manager) UpdateInboundLimiter(tag string, updatedSubscriptionList *[]api.SubscriptionInfo) error {
	return m.dispatcher.UpdateInboundLimiter(tag, updatedSubscriptionList)
}

func (m *Manager) UpdateNodeInfo(tag string, nodeSpeedLimit uint64, ignoreIPs []string) error {
	return m.dispatcher.UpdateNodeInfo(tag, nodeSpeedLimit, ignoreIPs)
}

func (m *Manager) DeleteInboundLimiter(tag string) error {
	return m.dispatcher.DeleteInboundLimiter(tag)
}

func (m *Manager) DeleteSubscriptionBuckets(tag string, emails []string) {
	m.dispatcher.DeleteSubscriptionBuckets(tag, emails)
}

func (m *Manager) GetOnlineIPs(tag string) (*[]api.OnlineIP, error) {
	return m.dispatcher.GetOnlineIPs(tag)
}

func (m *Manager) AddTag(nodeInfo *api.NodeInfo, rules []api.BlockingRule, tag string, config *Config, pusher func(string, any) error) error {
	inboundConfig, err := InboundBuilder(config, nodeInfo, tag, pusher, m.client)
	if err != nil {
		return fmt.Errorf("failed to build inbound config: %w", err)
	}
	if err := m.addInbound(inboundConfig); err != nil {
		return fmt.Errorf("failed to add inbound: %w", err)
	}

	outboundConfig, err := OutboundBuilder(config, nodeInfo, tag)
	if err != nil {
		return fmt.Errorf("failed to build outbound config: %w", err)
	}
	if err := m.addOutbound(outboundConfig); err != nil {
		return fmt.Errorf("failed to add outbound: %w", err)
	}

	if err := m.AddBlackHoleRuleTag(rules, tag); err != nil {
		return fmt.Errorf("failed to add blackhole rule: %w", err)
	}

	return nil
}

func (m *Manager) RemoveTag(tag string) error {
	if err := m.removeInbound(tag); err != nil {
		return fmt.Errorf("failed to remove inbound: %w", err)
	}
	if err := m.removeOutbound(tag); err != nil {
		return fmt.Errorf("failed to remove outbound: %w", err)
	}
	return nil
}

func (m *Manager) RemoveRelayTag(tag string, subscriptionInfo *[]api.SubscriptionInfo) error {
	for _, subscription := range *subscriptionInfo {
		outboundTag := fmt.Sprintf("%s_%d", tag, subscription.Id)
		if err := m.removeOutbound(outboundTag); err != nil {
			return err
		}
	}
	return nil
}

func blockRuleTag(tag string, gen, id, index int) string {
	if id <= 0 {
		id = index
	}
	return fmt.Sprintf("%s_blackhole_g%d_%d", tag, gen, id)
}

func relayRuleTag(relayTag string, gen, subID int) string {
	return fmt.Sprintf("%s_g%d_%d", relayTag, gen, subID)
}

func buildBlock(rules []api.BlockingRule, tag string, gen int) (*router.Config, []string, error) {
	if len(rules) == 0 {
		return nil, nil, nil
	}
	cfg, err := BlackHoleRouterBuilder(rules, tag, gen)
	if err != nil {
		return nil, nil, err
	}
	tags := make([]string, len(rules))
	for i, r := range rules {
		tags[i] = blockRuleTag(tag, gen, r.ID, i)
	}
	return cfg, tags, nil
}

func (m *Manager) removeRulesByTag(tags []string) error {
	var firstErr error
	for _, t := range tags {
		if err := m.removeRouterRule(t); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) AddBlackHoleRuleTag(rules []api.BlockingRule, tag string) error {
	blackholeConfig, err := BlackholeOutboundBuilder(tag)
	if err != nil {
		return fmt.Errorf("failed to build blackhole outbound: %w", err)
	}
	if err := m.addOutbound(blackholeConfig); err != nil {
		return fmt.Errorf("failed to add blackhole outbound: %w", err)
	}

	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	m.routeGen++
	cfg, tags, err := buildBlock(rules, tag, m.routeGen)
	if err != nil {
		return err
	}
	if cfg != nil {
		if err := m.addRouterRule(cfg, true); err != nil {
			return err
		}
	}
	m.blockMu.Lock()
	m.blockTags[tag] = tags
	m.blockMu.Unlock()
	return nil
}

func (m *Manager) RemoveBlockingRules(tag string) error {
	m.blockMu.Lock()
	tags := m.blockTags[tag]
	delete(m.blockTags, tag)
	m.blockMu.Unlock()

	firstErr := m.removeRulesByTag(tags)
	if err := m.removeOutbound(fmt.Sprintf("%s_blackhole", tag)); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (m *Manager) AddRelayTag(relayNodeInfo *api.RelayNodeInfo, relayTag string, mainTag string, subscriptionInfo *[]api.SubscriptionInfo) error {
	m.routeMu.Lock()
	m.routeGen++
	gen := m.routeGen
	set := &relayRuleSet{mainTag: mainTag}
	m.relays[relayTag] = set
	m.routeMu.Unlock()

	for _, subscription := range *subscriptionInfo {
		var key string
		if C.Contains(shadowaead_2022.List, strings.ToLower(relayNodeInfo.Cipher)) {
			userKey, err := CheckShadowsocksPassword(subscription.Passwd, relayNodeInfo.Cipher)
			if err != nil {
				continue
			}
			key = fmt.Sprintf("%s:%s", relayNodeInfo.ServerKey, userKey)
		} else {
			key = subscription.Passwd
		}

		relayTagConfig, err := OutboundRelayBuilder(relayNodeInfo, relayTag, &subscription, key)
		if err != nil {
			return fmt.Errorf("failed to build relay outbound for Id %d: %w", subscription.Id, err)
		}
		if err := m.addOutbound(relayTagConfig); err != nil {
			return fmt.Errorf("failed to add relay outbound for UID %d: %w", subscription.Id, err)
		}

		ruleTag := relayRuleTag(relayTag, gen, subscription.Id)
		routerConfig, err := RelayRouterBuilder(mainTag, relayTag, &subscription, ruleTag)
		if err != nil {
			return fmt.Errorf("failed to build router for UID %d: %w", subscription.Id, err)
		}
		if err := m.addRouterRule(routerConfig, true); err != nil {
			return fmt.Errorf("failed to add router rule for UID %d: %w", subscription.Id, err)
		}

		m.routeMu.Lock()
		set.subs = append(set.subs, subscription)
		set.tags = append(set.tags, ruleTag)
		m.routeMu.Unlock()
	}
	return nil
}

func (m *Manager) RemoveRelayRules(tag string, _ *[]api.SubscriptionInfo) error {
	m.routeMu.Lock()
	var tags []string
	if set := m.relays[tag]; set != nil {
		tags = append(tags, set.tags...)
	}
	delete(m.relays, tag)
	m.routeMu.Unlock()
	return m.removeRulesByTag(tags)
}

func (m *Manager) ReplaceBlockRules(tag string, rules []api.BlockingRule, relayTag string) error {
	m.routeMu.Lock()
	defer m.routeMu.Unlock()

	m.routeGen++
	gen := m.routeGen

	blockCfg, newBlockTags, err := buildBlock(rules, tag, gen)
	if err != nil {
		return err
	}
	set := m.relays[relayTag]
	var relayCfgs []*router.Config
	var newRelayTags []string
	if set != nil {
		for i := range set.subs {
			ruleTag := relayRuleTag(relayTag, gen, set.subs[i].Id)
			cfg, err := RelayRouterBuilder(set.mainTag, relayTag, &set.subs[i], ruleTag)
			if err != nil {
				return err
			}
			relayCfgs = append(relayCfgs, cfg)
			newRelayTags = append(newRelayTags, ruleTag)
		}
	}

	if blockCfg != nil {
		if err := m.addRouterRule(blockCfg, true); err != nil {
			return fmt.Errorf("add block rules: %w", err)
		}
	}
	for i, cfg := range relayCfgs {
		if err := m.addRouterRule(cfg, true); err != nil {
			_ = m.removeRulesByTag(newBlockTags)
			_ = m.removeRulesByTag(newRelayTags[:i])
			return fmt.Errorf("add relay rule: %w", err)
		}
	}

	m.blockMu.Lock()
	oldBlockTags := m.blockTags[tag]
	m.blockTags[tag] = newBlockTags
	m.blockMu.Unlock()

	var oldRelayTags []string
	if set != nil {
		oldRelayTags = set.tags
		set.tags = newRelayTags
	}

	firstErr := m.removeRulesByTag(oldRelayTags)
	if err := m.removeRulesByTag(oldBlockTags); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (m *Manager) removeInbound(tag string) error {
	return m.ibm.RemoveHandler(context.Background(), tag)
}

func (m *Manager) removeOutbound(tag string) error {
	return m.obm.RemoveHandler(context.Background(), tag)
}

func (m *Manager) addInbound(config *core.InboundHandlerConfig) error {
	rawHandler, err := core.CreateObject(m.server, config)
	if err != nil {
		return err
	}
	handler, ok := rawHandler.(inbound.Handler)
	if !ok {
		return fmt.Errorf("not an InboundHandler")
	}
	return m.ibm.AddHandler(context.Background(), handler)
}

func (m *Manager) addOutbound(config *core.OutboundHandlerConfig) error {
	rawHandler, err := core.CreateObject(m.server, config)
	if err != nil {
		return err
	}
	handler, ok := rawHandler.(outbound.Handler)
	if !ok {
		return fmt.Errorf("not an OutboundHandler")
	}
	return m.obm.AddHandler(context.Background(), handler)
}

func (m *Manager) addRouterRule(config *router.Config, shouldAppend bool) error {
	return m.router.AddRule(serial.ToTypedMessage(config), shouldAppend)
}

func (m *Manager) removeRouterRule(tag string) error {
	return m.router.RemoveRule(tag)
}

func CheckShadowsocksPassword(password string, method string) (string, error) {
	if len(password) < 16 {
		return "", fmt.Errorf("shadowsocks2022 key must be at least 16 characters")
	}
	var userKey string
	switch strings.ToLower(method) {
	case "2022-blake3-aes-128-gcm":
		userKey = password[:16]
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		if len(password) < 32 {
			return "", fmt.Errorf("shadowsocks2022 key must be at least 32 characters")
		}
		userKey = password[:32]
	default:
		return "", fmt.Errorf("unsupported SS2022 method: %s", method)
	}
	return base64.StdEncoding.EncodeToString([]byte(userKey)), nil
}

func buildQuicParams(q *api.QuicParamsSettings) *conf.QuicParamsConfig {
	qp := &conf.QuicParamsConfig{
		Congestion:                  q.Congestion,
		Debug:                       q.Debug,
		BbrProfile:                  q.BbrProfile,
		BrutalUp:                    conf.Bandwidth(q.BrutalUp),
		BrutalDown:                  conf.Bandwidth(q.BrutalDown),
		InitStreamReceiveWindow:     q.InitStreamReceiveWindow,
		MaxStreamReceiveWindow:      q.MaxStreamReceiveWindow,
		InitConnectionReceiveWindow: q.InitConnectionReceiveWindow,
		MaxConnectionReceiveWindow:  q.MaxConnectionReceiveWindow,
		MaxIdleTimeout:              q.MaxIdleTimeout,
		KeepAlivePeriod:             q.KeepAlivePeriod,
		DisablePathMTUDiscovery:     q.DisablePathMTUDiscovery,
		MaxIncomingStreams:          q.MaxIncomingStreams,
		BrutalDisableLossCompensation: q.BrutalDisableLossCompensation,
        DisableChromeParrot:         q.DisableChromeParrot,	
        DisableGSO:                  q.DisableGSO,
		DisableStatelessReset:       q.DisableStatelessReset,
	}

	return qp
}

func buildSocketConfig(s *api.SocketSettings, isInbound bool) *conf.SocketConfig {
	sc := &conf.SocketConfig{}
	if isInbound && s.AcceptProxyProtocol {
		sc.AcceptProxyProtocol = true
	}
	if s.DomainStrategy != "" {
		sc.DomainStrategy = s.DomainStrategy
	}
	if s.TCPKeepAliveInterval != 0 {
		sc.TCPKeepAliveInterval = s.TCPKeepAliveInterval
	}
	if s.TCPKeepAliveIdle != 0 {
		sc.TCPKeepAliveIdle = s.TCPKeepAliveIdle
	}
	if s.TCPUserTimeout != 0 {
		sc.TCPUserTimeout = s.TCPUserTimeout
	}
	if s.TCPMaxSeg != 0 {
		sc.TCPMaxSeg = s.TCPMaxSeg
	}
	if s.TCPWindowClamp != 0 {
		sc.TCPWindowClamp = s.TCPWindowClamp
	}
	if s.TcpMptcp {
		sc.TcpMptcp = true
	}
	if s.TcpCongestion != "" {
		sc.TCPCongestion = s.TcpCongestion
	}
	if s.V6only {
		sc.V6only = true
	}
	if s.TFO != nil {
		sc.TFO = s.TFO
	}
	if s.AddressPortStrategy != "" {
		sc.AddressPortStrategy = s.AddressPortStrategy
	}
	if isInbound && len(s.TrustedXForwardedFor) > 0 {
		sc.TrustedXForwardedFor = s.TrustedXForwardedFor
	}
	return sc
}

