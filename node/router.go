package node

import (
	"encoding/json"
	"fmt"
	
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xmplusdev/xmplus-server/api"
)

func RelayRouterBuilder(tag string, relayTag string, subscription *api.SubscriptionInfo) (*router.Config, error) {
	// Add nil check
	if subscription == nil {
		return nil, fmt.Errorf("subscription is nil")
	}
	
	routerConfig := &conf.RouterConfig{}
	var ruleList any
	
	User := conf.StringList{fmt.Sprintf("%s|%s|%d", tag, subscription.Email, subscription.Id)}
	InboundTag := conf.StringList{tag}
	
	ruleList = struct {
		RuleTag     string           `json:"ruleTag"`
		Type        string           `json:"type"`
		InboundTag  *conf.StringList `json:"inboundTag"`
		OutboundTag string           `json:"outboundTag"`
		User        *conf.StringList `json:"user"`
	}{
		RuleTag:     fmt.Sprintf("%s_%d", relayTag, subscription.Id),
		Type:        "field",
		InboundTag:  &InboundTag,
		OutboundTag: fmt.Sprintf("%s_%d", relayTag, subscription.Id),
		User:        &User,
	}
		
	rule, err := json.Marshal(ruleList)
	if err != nil {
		return nil, fmt.Errorf("Marshal Rule list %s config failed: %s", ruleList, err)
	}
		
	RuleList := []json.RawMessage{}
	RuleList = append(RuleList, rule)
	routerConfig.RuleList = RuleList
	return routerConfig.Build()
}

func DefaultRouterBuilder(tag string) (*router.Config, error) {
	routerConfig := &conf.RouterConfig{}
	RuleList := []json.RawMessage{}
	
	InboundTag := conf.StringList{tag}
	
	// Add default rule to route all other traffic to the main outbound
	// IMPORTANT: Only match traffic from this inbound to avoid interfering with relay user-specific rules
	//network := conf.NetworkList([]conf.Network{"tcp", "udp"})
	defaultRule := struct {
		Type        string            `json:"type"`
		RuleTag     string            `json:"ruleTag"`
		InboundTag  *conf.StringList `json:"inboundTag"`
		OutboundTag string            `json:"outboundTag"`
		//Network     *conf.NetworkList `json:"network,omitempty"`
	}{
		Type:        "field",
		RuleTag:     fmt.Sprintf("%s_default", tag),
		InboundTag:  &InboundTag,
		OutboundTag: tag,
		//Network:     &network,
	}
		
	rule, err := json.Marshal(defaultRule)
	if err != nil {
		return nil, fmt.Errorf("Marshal default rule config failed: %s", err)
	}
		
	RuleList = append(RuleList, rule)
	
	routerConfig.RuleList = RuleList
	return routerConfig.Build()
}

func RouterBuilder(nodeInfo *api.NodeInfo, tag string) (*router.Config, error) {
	// Add nil check
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}
	
	routerConfig := &conf.RouterConfig{}
	RuleList := []json.RawMessage{}
	
	// Only add blocking rule if there are actual blocking rules defined
	// First check if BlockingRules itself is not nil
	hasBlockingRules := false
	if nodeInfo.BlockingRules != nil {
		if (nodeInfo.BlockingRules.Port != "" && nodeInfo.BlockingRules.Port != "0") ||
		   (nodeInfo.BlockingRules.Domain != nil && len(nodeInfo.BlockingRules.Domain) > 0) ||
		   (nodeInfo.BlockingRules.IP != nil && len(nodeInfo.BlockingRules.IP) > 0) ||
		   (nodeInfo.BlockingRules.Protocol != nil && len(nodeInfo.BlockingRules.Protocol) > 0) {
			hasBlockingRules = true
		}
	}
	
	if hasBlockingRules {
		InboundTag := conf.StringList{tag}
		// Parse port string into PortRange slice
		var portRanges []conf.PortRange
		var err error
		if nodeInfo.BlockingRules.Port != "" && nodeInfo.BlockingRules.Port != "0" {
			portRanges, err = parsePortString(nodeInfo.BlockingRules.Port)
			if err != nil {
				return nil, fmt.Errorf("failed to parse port string: %w", err)
			}
		}
		
		var domain *conf.StringList
		if nodeInfo.BlockingRules.Domain != nil && len(nodeInfo.BlockingRules.Domain) > 0 {
			d := conf.StringList(nodeInfo.BlockingRules.Domain)
			domain = &d
		}

		var ip *conf.StringList
		if nodeInfo.BlockingRules.IP != nil && len(nodeInfo.BlockingRules.IP) > 0 {
			i := conf.StringList(nodeInfo.BlockingRules.IP)
			ip = &i
		}

		var protocols *conf.StringList
		if nodeInfo.BlockingRules.Protocol != nil && len(nodeInfo.BlockingRules.Protocol) > 0 {
			p := conf.StringList(nodeInfo.BlockingRules.Protocol)
			protocols = &p
		}
		
		var portList *conf.PortList
		if len(portRanges) > 0 {
			portList = &conf.PortList{Range: portRanges}
		}
		
		blockingRule := struct {
			Type        string           `json:"type"`
			RuleTag     string           `json:"ruleTag"`
			InboundTag  *conf.StringList `json:"inboundTag"`
			OutboundTag string           `json:"outboundTag"`
			Domain      *conf.StringList `json:"domain,omitempty"`
			IP          *conf.StringList `json:"ip,omitempty"`
			Port        *conf.PortList   `json:"port,omitempty"`
			Protocols   *conf.StringList `json:"protocol,omitempty"`
		}{
			Type:        "field",
			RuleTag:     fmt.Sprintf("%s_blackhole", tag),
		    InboundTag:  &InboundTag,
			OutboundTag: fmt.Sprintf("%s_blackhole", tag),
			Domain:      domain,
			IP:          ip,
			Protocols:   protocols,
			Port:        portList,
		}
		
		rule, err := json.Marshal(blockingRule)
		if err != nil {
			return nil, fmt.Errorf("Marshal blocking rule config failed: %s", err)
		}
		
		RuleList = append(RuleList, rule)
	}
	
	routerConfig.RuleList = RuleList
	return routerConfig.Build()
}
