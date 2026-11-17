package node

import (
	"encoding/json"
	"fmt"

	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xmplusdev/xmray/api"
)

func RelayRouterBuilder(tag string, relayTag string, subscription *api.SubscriptionInfo) (*router.Config, error) {
	if subscription == nil {
		return nil, fmt.Errorf("subscription is nil")
	}

	routerConfig := &conf.RouterConfig{}
	User := conf.StringList{fmt.Sprintf("%s|%s|%d", tag, subscription.Email, subscription.Id)}
	InboundTag := conf.StringList{tag}

	ruleList := struct {
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
		return nil, fmt.Errorf("marshal relay rule config failed: %s", err)
	}

	routerConfig.RuleList = []json.RawMessage{rule}
	return routerConfig.Build()
}

func DefaultRouterBuilder(tag string) (*router.Config, error) {
	routerConfig := &conf.RouterConfig{}
	InboundTag := conf.StringList{tag}

	defaultRule := struct {
		Type        string           `json:"type"`
		RuleTag     string           `json:"ruleTag"`
		InboundTag  *conf.StringList `json:"inboundTag"`
		OutboundTag string           `json:"outboundTag"`
	}{
		Type:        "field",
		RuleTag:     fmt.Sprintf("%s_default", tag),
		InboundTag:  &InboundTag,
		OutboundTag: tag,
	}

	rule, err := json.Marshal(defaultRule)
	if err != nil {
		return nil, fmt.Errorf("marshal default rule config failed: %s", err)
	}

	routerConfig.RuleList = []json.RawMessage{rule}
	return routerConfig.Build()
}

func BlackHoleRouterBuilder(nodeInfo *api.NodeInfo, tag string) (*router.Config, error) {
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}

	strList := func(v []string) *conf.StringList {
		if len(v) == 0 {
			return nil
		}
		l := conf.StringList(v)
		return &l
	}

	networkList := func(v []string) *conf.NetworkList {
		if len(v) == 0 {
			return nil
		}
		l := make(conf.NetworkList, 0, len(v))
		for _, n := range v {
			l = append(l, conf.Network(n))
		}
		return &l
	}

	routerConfig := &conf.RouterConfig{}
	inboundTag := conf.StringList{tag}
	outboundTag := fmt.Sprintf("%s_blackhole", tag)

	for i, r := range nodeInfo.BlockingRules {
		id := r.ID
		if id <= 0 {
			id = i
		}

		var portList *conf.PortList
		if len(r.Port) > 0 {
			ranges, err := api.ParsePorts(r.Port)
			if err != nil {
				return nil, fmt.Errorf("blocking rule %d: failed to parse port: %w", r.ID, err)
			}
			portList = &conf.PortList{Range: ranges}
		}

		blockingRule := struct {
			Type        string            `json:"type"`
			RuleTag     string            `json:"ruleTag"`
			InboundTag  *conf.StringList  `json:"inboundTag"`
			OutboundTag string            `json:"outboundTag"`
			Domain      *conf.StringList  `json:"domain,omitempty"`
			IP          *conf.StringList  `json:"ip,omitempty"`
			Network     *conf.NetworkList `json:"network,omitempty"`
			Port        *conf.PortList    `json:"port,omitempty"`
			Protocols   *conf.StringList  `json:"protocol,omitempty"`
		}{
			Type:        "field",
			RuleTag:     fmt.Sprintf("%s_blackhole_%d", tag, id),
			InboundTag:  &inboundTag,
			OutboundTag: outboundTag,
			Domain:      strList(r.Domain),
			IP:          strList(r.IP),
			Network:     networkList(r.Network),
			Port:        portList,
			Protocols:   strList(r.Protocol),
		}

		rule, err := json.Marshal(blockingRule)
		if err != nil {
			return nil, fmt.Errorf("marshal blocking rule %d failed: %w", r.ID, err)
		}
		routerConfig.RuleList = append(routerConfig.RuleList, rule)
	}

	return routerConfig.Build()
}
