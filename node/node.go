package node

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/xmplusdev/xmplus-server/api"
	"github.com/xmplusdev/xmplus-server/helper/limiter"
	"github.com/xmplusdev/xmplus-server/app/dispatcher" 
	
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/infra/conf"
	
	C "github.com/sagernet/sing/common"
	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
)

// Manager handles node-related operations (inbound/outbound management)
type Manager struct {
	server 		*core.Instance
	ibm    		inbound.Manager
	obm    		outbound.Manager
	router  	*router.Router
	dispatcher  *dispatcher.DefaultDispatcher
}

// NewManager creates a new node manager
func NewManager(server *core.Instance) *Manager {
	return &Manager{
		server: 	server,
		ibm:    	server.GetFeature(inbound.ManagerType()).(inbound.Manager),
		obm:    	server.GetFeature(outbound.ManagerType()).(outbound.Manager),
		router: 	server.GetFeature(routing.RouterType()).(*router.Router),
		dispatcher: server.GetFeature(routing.DispatcherType()).(*dispatcher.DefaultDispatcher),
	}
}

// parsePortString parses a port string like "53", "1000-2000", or "53,443,1000-2000" into PortRange slices
// This is a utility function used by inbound.go and router.go
func parsePortString(portStr string) ([]conf.PortRange, error) {
	if portStr == "" {
		return nil, fmt.Errorf("port string is empty")
	}
	
	var portRanges []conf.PortRange
	
	// Check if it contains comma (multiple ports/ranges)
	if strings.Contains(portStr, ",") {
		// Split by comma
		ports := strings.Split(portStr, ",")
		
		for _, p := range ports {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			
			// Check if it's a range (contains "-")
			if strings.Contains(p, "-") {
				rangeParts := strings.SplitN(p, "-", 2)
				if len(rangeParts) != 2 {
					return nil, fmt.Errorf("invalid port range format: %s", p)
				}
				
				fromPort, err := strconv.ParseUint(strings.TrimSpace(rangeParts[0]), 10, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid port number in range: %s", rangeParts[0])
				}
				
				toPort, err := strconv.ParseUint(strings.TrimSpace(rangeParts[1]), 10, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid port number in range: %s", rangeParts[1])
				}
				
				if(fromPort > toPort){
					return nil, fmt.Errorf("Starting 【%s】 port cannot be greater than ending 【%s】 port.", rangeParts[0], rangeParts[1])
				}
				
				portRanges = append(portRanges, conf.PortRange{
					From: uint32(fromPort),
					To:   uint32(toPort),
				})
			} else {
				// Single port
				port, err := strconv.ParseUint(p, 10, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid port number: %s", p)
				}
				
				portRanges = append(portRanges, conf.PortRange{
					From: uint32(port),
					To:   uint32(port),
				})
			}
		}
	} else if strings.Contains(portStr, "-") {
		// Single range (e.g., "1000-2000")
		rangeParts := strings.SplitN(portStr, "-", 2)
		if len(rangeParts) != 2 {
			return nil, fmt.Errorf("invalid port range format: %s", portStr)
		}
		
		fromPort, err := strconv.ParseUint(strings.TrimSpace(rangeParts[0]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid port number in range: %s", rangeParts[0])
		}
		
		toPort, err := strconv.ParseUint(strings.TrimSpace(rangeParts[1]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid port number in range: %s", rangeParts[1])
		}
		
		if(fromPort > toPort){
			return nil, fmt.Errorf("Starting 【%s】 port cannot be greater than ending 【%s】 port.", rangeParts[0], rangeParts[1])
		}
				
		portRanges = append(portRanges, conf.PortRange{
			From: uint32(fromPort),
			To:   uint32(toPort),
		})
	} else {
		// Single port (e.g., "443")
		port, err := strconv.ParseUint(portStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid port number: %s", portStr)
		}
		
		portRanges = append(portRanges, conf.PortRange{
			From: uint32(port),
			To:   uint32(port),
		})
	}
	
	return portRanges, nil
}

// AddTag adds both inbound and outbound for a node
func (m *Manager) AddTag(nodeInfo *api.NodeInfo, tag string, config *Config) error {
	if nodeInfo.NodeType == "Shadowsocks-Plugin" {
		return fmt.Errorf("Inbound server with type %s is not supportted", nodeInfo.NodeType)
	}

	// Add inbound
	inboundConfig, err := InboundBuilder(config, nodeInfo, tag)
	if err != nil {
		return fmt.Errorf("failed to build inbound config: %w", err)
	}

	if err := m.addInbound(inboundConfig); err != nil {
		return fmt.Errorf("failed to add inbound: %w", err)
	}

	// Add outbound
	outboundConfig, err := OutboundBuilder(config, nodeInfo, tag)
	if err != nil {
		return fmt.Errorf("failed to build outbound config: %w", err)
	}

	if err := m.addOutbound(outboundConfig); err != nil {
		return fmt.Errorf("failed to add outbound: %w", err)
	}
	
	if nodeInfo.RelayType == 0 || nodeInfo.RelayNodeID == 0 {
		// Build and add router rule
		routerConfig, err := DefaultRouterBuilder(tag)
		if err != nil {
			return err
		}
		
		// Add rule
		if err := m.addRouterRule(routerConfig, true); err != nil {
			return err
		}
	}

	//log.Printf("Added inbound tag %s for node type %s", tag, nodeInfo.NodeType)
	return nil
}

// RemoveTag removes both inbound and outbound for a node
func (m *Manager) RemoveTag(tag string) error {
	if err := m.removeInbound(tag); err != nil {
		return fmt.Errorf("failed to remove inbound: %w", err)
	}

	if err := m.removeOutbound(tag); err != nil {
		return fmt.Errorf("failed to remove outbound: %w", err)
	}
	
	defaultRuleTag := fmt.Sprintf("%s_default", tag)
	if errr := m.removeRouterRule(defaultRuleTag); errr != nil {
		return errr
	}
	
	return nil
}

// AddRelayTag adds relay outbounds and routing for all subscriptions
func (m *Manager) AddRelayTag(
	relayNodeInfo *api.RelayNodeInfo,
	relayTag string,
	mainTag string,
	subscriptionInfo *[]api.SubscriptionInfo,
) error {
	if relayNodeInfo.NodeType == "Shadowsocks-Plugin" {
		return fmt.Errorf("Rely outbound server with type %s is not supportted", relayNodeInfo.NodeType)
	}

	for _, subscription := range *subscriptionInfo {
		var key string

		// Handle Shadowsocks 2022 key generation
		if C.Contains(shadowaead_2022.List, strings.ToLower(relayNodeInfo.Cipher)) {
			userKey, err := checkShadowsocksPassword(subscription.Passwd, relayNodeInfo.Cipher)
			if err != nil {
				continue
			}
			key = fmt.Sprintf("%s:%s", relayNodeInfo.ServerKey, userKey)
		} else {
			key = subscription.Passwd
		}

		// Build and add relay outbound
		relayTagConfig, err := OutboundRelayBuilder(relayNodeInfo, relayTag, &subscription, key)
		if err != nil {
			return fmt.Errorf("failed to build relay outbound for Id %d: %w", subscription.Id, err)
		}

		if err := m.addOutbound(relayTagConfig); err != nil {
			return fmt.Errorf("failed to add relay outbound for UID %d: %w", subscription.Id, err)
		}

		// Build and add router rule
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

func checkShadowsocksPassword(password string, method string) (string, error) {
	var userKey string
	if len(password) < 16 {
		return "", fmt.Errorf("shadowsocks2022 key's length must be greater than 16")
	}
	
	switch strings.ToLower(method) {
		case "2022-blake3-aes-128-gcm":
			userKey = password[:16]
		case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
			if len(password) < 32 {
				return "", fmt.Errorf("shadowsocks2022 key's length must be greater than 32")
			}
			userKey = password[:32]
		default:
			return "", fmt.Errorf("unsupported SS2022 method: %s", method)	
	}
	
	return base64.StdEncoding.EncodeToString([]byte(userKey)), nil
}

// RemoveRelayTag removes all relay outbounds for subscriptions
func (m *Manager) RemoveRelayTag(tag string, subscriptionInfo *[]api.SubscriptionInfo) error {
	for _, subscription := range *subscriptionInfo {
		outboundTag := fmt.Sprintf("%s_%d", tag, subscription.Id)
		if err := m.removeOutbound(outboundTag); err != nil {
			return err
		}
	}

	return nil
}

// RemoveRelayRules removes all routing rules for relay
func (m *Manager) RemoveRelayRules(tag string, subscriptionInfo *[]api.SubscriptionInfo) error {
	for _, subscription := range *subscriptionInfo {
		ruleTag := fmt.Sprintf("%s_%d", tag, subscription.Id)
		if err := m.removeRouterRule(ruleTag); err != nil {
			return err
		}
	}

	return nil
}

// RemoveBlockingRules removes all routing rules for relay
func (m *Manager) RemoveBlockingRules(tag string) error {
	ruleTag := fmt.Sprintf("%s_blackhole", tag)
	if err := m.removeRouterRule(ruleTag); err != nil {
		return err
	}
	
	if err := m.removeOutbound(ruleTag); err != nil {
		return err
	}

	return nil
}

// Add blocking rule Tag for outbound 
func (m *Manager) AddRuleTag(nodeInfo *api.NodeInfo, tag string) error {
	// Add outbound
	blackholeConfig, err := BlackholeOutboundBuilder(tag)
	if err != nil {
		return fmt.Errorf("failed to build outbound config: %w", err)
	}

	if err := m.addOutbound(blackholeConfig); err != nil {
		return fmt.Errorf("failed to add outbound: %w", err)
	}
	
	// Build and add router rule
	routerConfig, err := RouterBuilder(nodeInfo, tag)
	if err != nil {
		return err
	}

	if err := m.addRouterRule(routerConfig, true); err != nil {
		return err
	}
	
	return nil
}

// Private helper methods
func (m *Manager) removeInbound(tag string) error {
	err := m.ibm.RemoveHandler(context.Background(), tag)
	return err
}

func (m *Manager) removeOutbound(tag string) error {
	err := m.obm.RemoveHandler(context.Background(), tag)
	return err
}

func (m *Manager) addInbound(config *core.InboundHandlerConfig) error {
	rawHandler, err := core.CreateObject(m.server, config)
	if err != nil {
		return err
	}
	handler, ok := rawHandler.(inbound.Handler)
	if !ok {
		return fmt.Errorf("not an InboundHandler: %s", err)
	}
	if err := m.ibm.AddHandler(context.Background(), handler); err != nil {
		return err
	}
	return nil
}

func (m *Manager) addOutbound(config *core.OutboundHandlerConfig) error {
	rawHandler, err := core.CreateObject(m.server, config)
	if err != nil {
		return err
	}
	handler, ok := rawHandler.(outbound.Handler)
	if !ok {
		return fmt.Errorf("not an InboundHandler: %s", err)
	}
	if err := m.obm.AddHandler(context.Background(), handler); err != nil {
		return err
	}
	return nil
}

func (m *Manager) addRouterRule(config *router.Config, shouldAppend bool) error{
	err := m.router.AddRule(serial.ToTypedMessage(config), shouldAppend)
	return err
}

func (m *Manager) removeRouterRule(tag string) error{
	err := m.router.RemoveRule(tag)
	return err
}

func (m *Manager) AddInboundLimiter(tag string, expiry int, nodeSpeedLimit uint64, subscriptionList *[]api.SubscriptionInfo, redisConfig *limiter.RedisConfig) error {
	err := m.dispatcher.Limiter.AddInboundLimiter(tag, expiry, nodeSpeedLimit, subscriptionList, redisConfig)
	return err
}

func (m *Manager) UpdateInboundLimiter(tag string, updatedSubscriptionList *[]api.SubscriptionInfo) error {
	err := m.dispatcher.Limiter.UpdateInboundLimiter(tag, updatedSubscriptionList)
	return err
}

func (m *Manager) DeleteInboundLimiter(tag string) error {
	err := m.dispatcher.Limiter.DeleteInboundLimiter(tag)
	return err
}