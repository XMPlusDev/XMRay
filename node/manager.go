package node

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

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
	"github.com/xmplusdev/xmray/dispatcher"
)

type Manager struct {
	server     *core.Instance
	ibm        inbound.Manager
	obm        outbound.Manager
	router     *router.Router
	dispatcher *dispatcher.LimitingDispatcher
}

func NewManager(server *core.Instance, d *dispatcher.LimitingDispatcher) *Manager {
	return &Manager{
		server:     server,
		ibm:        server.GetFeature(inbound.ManagerType()).(inbound.Manager),
		obm:        server.GetFeature(outbound.ManagerType()).(outbound.Manager),
		router:     server.GetFeature(routing.RouterType()).(*router.Router),
		dispatcher: d,
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

func (m *Manager) AddTag(nodeInfo *api.NodeInfo, tag string, config *Config) error {
	inboundConfig, err := InboundBuilder(config, nodeInfo, tag)
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

	if nodeInfo.RelayType == 0 || nodeInfo.RelayNodeID == 0 {
		routerConfig, err := DefaultRouterBuilder(tag)
		if err != nil {
			return err
		}
		if err := m.addRouterRule(routerConfig, true); err != nil {
			return err
		}
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
	defaultRuleTag := fmt.Sprintf("%s_default", tag)
	return m.removeRouterRule(defaultRuleTag)
}

func (m *Manager) AddRelayTag(relayNodeInfo *api.RelayNodeInfo, relayTag string, mainTag string, subscriptionInfo *[]api.SubscriptionInfo) error {
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

		routerConfig, err := RelayRouterBuilder(mainTag, relayTag, &subscription)
		if err != nil {
			return fmt.Errorf("failed to build router for UID %d: %w", subscription.Id, err)
		}
		if err := m.addRouterRule(routerConfig, true); err != nil {
			return fmt.Errorf("failed to add router rule for UID %d: %w", subscription.Id, err)
		}
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

func (m *Manager) RemoveRelayRules(tag string, subscriptionInfo *[]api.SubscriptionInfo) error {
	for _, subscription := range *subscriptionInfo {
		ruleTag := fmt.Sprintf("%s_%d", tag, subscription.Id)
		if err := m.removeRouterRule(ruleTag); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) RemoveBlockingRules(tag string) error {
	ruleTag := fmt.Sprintf("%s_blackhole", tag)
	if err := m.removeRouterRule(ruleTag); err != nil {
		return err
	}
	return m.removeOutbound(ruleTag)
}

func (m *Manager) AddBlackHoleRuleTag(nodeInfo *api.NodeInfo, tag string) error {
	blackholeConfig, err := BlackholeOutboundBuilder(tag)
	if err != nil {
		return fmt.Errorf("failed to build blackhole outbound: %w", err)
	}
	if err := m.addOutbound(blackholeConfig); err != nil {
		return fmt.Errorf("failed to add blackhole outbound: %w", err)
	}

	routerConfig, err := BlackHoleRouterBuilder(nodeInfo, tag)
	if err != nil {
		return err
	}
	return m.addRouterRule(routerConfig, true)
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

// parsePortString is also needed by router.go in this package.
func parsePortString(portStr string) ([]conf.PortRange, error) {
	if portStr == "" {
		return nil, fmt.Errorf("port string is empty")
	}
	var portRanges []conf.PortRange
	for _, segment := range strings.Split(portStr, ",") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if strings.Contains(segment, "-") {
			parts := strings.SplitN(segment, "-", 2)
			from, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid port in range: %s", parts[0])
			}
			to, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid port in range: %s", parts[1])
			}
			if from > to {
				return nil, fmt.Errorf("start port %d > end port %d", from, to)
			}
			portRanges = append(portRanges, conf.PortRange{From: uint32(from), To: uint32(to)})
		} else {
			port, err := strconv.ParseUint(segment, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid port number: %s", segment)
			}
			portRanges = append(portRanges, conf.PortRange{From: uint32(port), To: uint32(port)})
		}
	}
	return portRanges, nil
}

// CheckShadowsocksPassword validates and derives the SS2022 user key.
// Exported so that subscription/builders.go can call it instead of duplicating.
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
	if isInbound && len(s.TrustedXForwardedFor) > 0 {
		sc.TrustedXForwardedFor = s.TrustedXForwardedFor
	}
	return sc
}
