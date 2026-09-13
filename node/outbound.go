package node

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"

	"github.com/xmplusdev/xmray/api"
)

func OutboundBuilder(config *Config, nodeInfo *api.NodeInfo, tag string) (*core.OutboundHandlerConfig, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}

	outboundDetourConfig := &conf.OutboundDetourConfig{}
	outboundDetourConfig.Protocol = "freedom"
	outboundDetourConfig.Tag = tag

	if nodeInfo.SendThroughIP != "" {
		outboundDetourConfig.SendThrough = &nodeInfo.SendThroughIP
	}

	domainStrategy := "Asis"
	if config.DNSConfig != nil && config.DNSConfig.Enable && config.DNSConfig.Strategy != "" {
		domainStrategy = config.DNSConfig.Strategy
	}

	proxySetting := &conf.FreedomConfig{DomainStrategy: domainStrategy}
	for _, fr := range nodeInfo.FinalRules {
		if rule := buildFinalRule(fr); rule != nil {
			proxySetting.FinalRules = append(proxySetting.FinalRules, rule)
		}
	}

	settingBytes, err := json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy config failed: %s", err)
	}
	setting := json.RawMessage(settingBytes)
	outboundDetourConfig.Settings = &setting

	if nodeInfo.SocketSettings != nil && nodeInfo.SocketSettings.Enabled {
		outboundDetourConfig.StreamSetting = &conf.StreamConfig{
			SocketSettings: buildSocketConfig(nodeInfo.SocketSettings, false),
		}
	}

	return outboundDetourConfig.Build()
}

func BlackholeOutboundBuilder(tag string) (*core.OutboundHandlerConfig, error) {
	outboundDetourConfig := &conf.OutboundDetourConfig{}
	outboundDetourConfig.Protocol = "blackhole"
	outboundDetourConfig.Tag = fmt.Sprintf("%s_blackhole", tag)
	
	rc := &conf.ResponseConfig{
		Type: "http",
	}
	
	blackholeSetting := &conf.BlackholeConfig{Response: rc}
	settingBytes, err := json.Marshal(blackholeSetting)
	if err != nil {
		return nil, fmt.Errorf("marshal blackhole config failed: %s", err)
	}
	setting := json.RawMessage(settingBytes)
	outboundDetourConfig.Settings = &setting

	return outboundDetourConfig.Build()
}

func OutboundRelayBuilder(nodeInfo *api.RelayNodeInfo, tag string, subscription *api.SubscriptionInfo, Passwd string) (*core.OutboundHandlerConfig, error) {
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}
	if subscription == nil {
		return nil, fmt.Errorf("subscription is nil")
	}

	outboundDetourConfig := &conf.OutboundDetourConfig{}
	var proxySetting any

	switch nodeInfo.NodeType {
	case "vless":
		outboundDetourConfig.Protocol = "vless"
		vUser, err := vlessUser(tag, nodeInfo, subscription)
		if err != nil {
			return nil, fmt.Errorf("marshal vless user config failed: %s", err)
		}
		proxySetting = struct {
			Vnext []*conf.VLessOutboundVnext `json:"vnext"`
		}{
			Vnext: []*conf.VLessOutboundVnext{{
				Address: &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
				Port:    uint16(nodeInfo.Port),
				Users:   []json.RawMessage{vUser},
			}},
		}

	case "vmess":
		outboundDetourConfig.Protocol = "vmess"
		userVmess, err := vmessUser(tag, subscription)
		if err != nil {
			return nil, fmt.Errorf("marshal vmess user config failed: %s", err)
		}
		proxySetting = struct {
			Receivers []*conf.VMessOutboundTarget `json:"vnext"`
		}{
			Receivers: []*conf.VMessOutboundTarget{{
				Address: &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
				Port:    uint16(nodeInfo.Port),
				Users:   []json.RawMessage{userVmess},
			}},
		}

	case "trojan":
		outboundDetourConfig.Protocol = "trojan"
		proxySetting = struct {
			Servers []*conf.TrojanServerTarget `json:"servers"`
		}{
			Servers: []*conf.TrojanServerTarget{{
				Address:  &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
				Port:     uint16(nodeInfo.Port),
				Password: subscription.Passwd,
				Email:    fmt.Sprintf("%s_%s", tag, subscription.Email),
			}},
		}

	case "shadowsocks":
		outboundDetourConfig.Protocol = "shadowsocks"
		proxySetting = struct {
			Servers []*conf.ShadowsocksServerTarget `json:"servers"`
		}{
			Servers: []*conf.ShadowsocksServerTarget{{
				Address:  &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
				Port:     uint16(nodeInfo.Port),
				Password: Passwd,
				Email:    fmt.Sprintf("%s_%s", tag, subscription.Email),
				Cipher:   nodeInfo.Cipher,
			}},
		}

	case "hysteria":
		outboundDetourConfig.Protocol = "hysteria"
		proxySetting = struct {
			*conf.HysteriaClientConfig
		}{
			&conf.HysteriaClientConfig{
				Address: &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
				Port:    uint16(nodeInfo.Port),
				Version: nodeInfo.HysteriaSettings.Version,
			},
		}

	default:
		return nil, fmt.Errorf("unsupported relay node type: %s", nodeInfo.NodeType)
	}

	settingBytes, err := json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy %s config failed: %s", nodeInfo.NodeType, err)
	}
	setting := json.RawMessage(settingBytes)
	outboundDetourConfig.Settings = &setting

	streamSetting := new(conf.StreamConfig)
	transportProtocol := conf.TransportProtocol(nodeInfo.NetworkType)
	networkType, err := transportProtocol.Build()
	if err != nil {
		return nil, fmt.Errorf("convert TransportProtocol failed: %s", err)
	}
	streamSetting.Network = &transportProtocol

	switch networkType {
	case "hysteria", "hysteria2":
		hysteriaSettings := &conf.HysteriaConfig{}
		if nodeInfo.HysteriaSettings != nil {
			hysteriaSettings.Version = nodeInfo.HysteriaSettings.Version
			hysteriaSettings.Auth = subscription.Passwd
		}
		streamSetting.HysteriaSettings = hysteriaSettings

	case "xhttp", "splithttp":
		xhttpSettings := &conf.SplitHTTPConfig{}
		if nodeInfo.XhttpSettings != nil {
			x := nodeInfo.XhttpSettings
			xhttpSettings.Host = x.Host
			xhttpSettings.Path = x.Path
			xhttpSettings.Mode = x.Mode
			if x.XPaddingObfsMode || x.Xmux != (api.XmuxConfig{}) {
				maxConcurrency, err := parseInt32Range(x.Xmux.MaxConcurrency, 0, 0)
				if err != nil {
					return nil, fmt.Errorf("Xmux.MaxConcurrency: %w", err)
				}
				maxConnections, err := parseInt32Range(x.Xmux.MaxConnections, 0, 0)
				if err != nil {
					return nil, fmt.Errorf("Xmux.MaxConnections: %w", err)
				}
				cMaxReuseTimes, err := parseInt32Range(x.Xmux.CMaxReuseTimes, 0, 0)
				if err != nil {
					return nil, fmt.Errorf("Xmux.CMaxReuseTimes: %w", err)
				}
				hMaxRequestTimes, err := parseInt32Range(x.Xmux.HMaxRequestTimes, 0, 0)
				if err != nil {
					return nil, fmt.Errorf("Xmux.HMaxRequestTimes: %w", err)
				}
				hMaxReusableSecs, err := parseInt32Range(x.Xmux.HMaxReusableSecs, 0, 0)
				if err != nil {
					return nil, fmt.Errorf("Xmux.HMaxReusableSecs: %w", err)
				}
				extra := struct {
					XPaddingObfsMode    bool            `json:"xPaddingObfsMode"`
					XPaddingMethod      string          `json:"xPaddingMethod,omitempty"`
					XPaddingPlacement   string          `json:"xPaddingPlacement,omitempty"`
					XPaddingKey         string          `json:"xPaddingKey,omitempty"`
					XPaddingHeader      string          `json:"xPaddingHeader,omitempty"`
					UplinkHTTPMethod    string          `json:"uplinkHTTPMethod,omitempty"`
					SessionIDPlacement  string          `json:"sessionIDPlacement,omitempty"`
					SessionIDKey        string          `json:"sessionIDKey,omitempty"`
					SessionIDTable      string          `json:"sessionIDTable,omitempty"`
					SessionIDLength     string          `json:"sessionIDLength,omitempty"`
					SeqPlacement        string          `json:"seqPlacement,omitempty"`
					SeqKey              string          `json:"seqKey,omitempty"`
					UplinkDataPlacement string          `json:"uplinkDataPlacement,omitempty"`
					UplinkDataKey       string          `json:"uplinkDataKey,omitempty"`
					UplinkChunkSize     string          `json:"uplinkChunkSize,omitempty"`
					Xmux                conf.XmuxConfig `json:"xmux"`
				}{
					XPaddingObfsMode:    x.XPaddingObfsMode,
					XPaddingMethod:      x.XPaddingMethod,
					XPaddingPlacement:   x.XPaddingPlacement,
					XPaddingKey:         x.XPaddingKey,
					XPaddingHeader:      x.XPaddingHeader,
					UplinkHTTPMethod:    x.UplinkHTTPMethod,
					SessionIDPlacement:  x.SessionIDPlacement,
					SessionIDKey:        x.SessionIDKey,
					SessionIDTable:      x.SessionIDTable,
					SessionIDLength:     x.SessionIDLength,
					SeqPlacement:        x.SeqPlacement,
					SeqKey:              x.SeqKey,
					UplinkDataPlacement: x.UplinkDataPlacement,
					UplinkDataKey:       x.UplinkDataKey,
					UplinkChunkSize:     x.UplinkChunkSize,
					Xmux: conf.XmuxConfig{
						MaxConcurrency:   maxConcurrency,
						MaxConnections:   maxConnections,
						CMaxReuseTimes:   cMaxReuseTimes,
						HMaxRequestTimes: hMaxRequestTimes,
						HMaxReusableSecs: hMaxReusableSecs,
						HKeepAlivePeriod: x.Xmux.HKeepAlivePeriod,
					},
				}
				extraBytes, err := json.Marshal(extra)
				if err != nil {
					return nil, fmt.Errorf("marshal xhttp extra config failed: %w", err)
				}
				xhttpSettings.Extra = json.RawMessage(extraBytes)
			}
		}
		streamSetting.XHTTPSettings = xhttpSettings

	default:
		if err := applyCommonTransport(streamSetting, networkType,
			nodeInfo.RawSettings, nodeInfo.WsSettings, nodeInfo.HttpSettings,
			nodeInfo.GrpcSettings, nodeInfo.KcpSettings); err != nil {
			return nil, err
		}
	}

	applyMaskSettings(streamSetting, nodeInfo.MaskSettings)

	if nodeInfo.SocketSettings != nil && nodeInfo.SocketSettings.Enabled {
		streamSetting.SocketSettings = buildSocketConfig(nodeInfo.SocketSettings, false)
	}

	if nodeInfo.SecurityType == "tls" && nodeInfo.TlsSettings != nil {
		streamSetting.Security = "tls"
		streamSetting.TLSSettings = &conf.TLSConfig{
			ServerName:           nodeInfo.TlsSettings.ServerName,
			Fingerprint:          nodeInfo.TlsSettings.FingerPrint,
			VerifyPeerCertByName: nodeInfo.TlsSettings.VerifyPeerCertByName,
			ECHConfigList:        nodeInfo.TlsSettings.ECHConfigList,
			PinnedPeerCertSha256: nodeInfo.TlsSettings.PinnedPeerCertSha256,
		}
	}

	if nodeInfo.SecurityType == "reality" && nodeInfo.RealitySettings != nil {
		streamSetting.Security = "reality"
		rs := nodeInfo.RealitySettings
		streamSetting.REALITYSettings = &conf.REALITYConfig{
			Show:          rs.Show,
			ServerName:    rs.ServerName,
			PublicKey:     rs.PublicKey,
			Fingerprint:   rs.Fingerprint,
			ShortId:       rs.ShortId,
			SpiderX:       rs.SpiderX,
			Mldsa65Verify: rs.Mldsa65Verify,
		}
	}

	outboundDetourConfig.Tag = fmt.Sprintf("%s_%d", tag, subscription.Id)
	if nodeInfo.SendThroughIP != "" {
		outboundDetourConfig.SendThrough = &nodeInfo.SendThroughIP
	}
	outboundDetourConfig.StreamSetting = streamSetting

	return outboundDetourConfig.Build()
}

func buildFinalRule(fr api.FinalRuleSettings) *conf.FreedomFinalRuleConfig {
	if fr.Action == "" {
		return nil
	}
	rule := &conf.FreedomFinalRuleConfig{Action: fr.Action}

	if fr.Network != "" {
		nl := conf.NetworkList{}
		for _, n := range strings.Split(fr.Network, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				nl = append(nl, conf.Network(n))
			}
		}
		rule.Network = &nl
	}

	if fr.Port != "" {
		pl := &conf.PortList{}
		if err := json.Unmarshal([]byte(`"`+fr.Port+`"`), pl); err == nil {
			rule.Port = pl
		}
	}

	if len(fr.IP) > 0 {
		sl := conf.StringList(fr.IP)
		rule.IP = &sl
	}

	if fr.BlockDelay != "" {
		from, to, err := conf.ParseRangeString(fr.BlockDelay)
		if err == nil {
			r := conf.Int32Range{}
			r.Left = int32(from)
			r.Right = int32(to)
			if r.Left > r.Right {
				r.From, r.To = r.Right, r.Left
			} else {
				r.From, r.To = r.Left, r.Right
			}
			rule.BlockDelay = &r
		}
	}

	return rule
}

func vmessUser(tag string, subscription *api.SubscriptionInfo) (json.RawMessage, error) {
	if subscription == nil {
		return nil, fmt.Errorf("subscription is nil")
	}
	account := struct {
		Level    int    `json:"level"`
		Email    string `json:"email"`
		ID       string `json:"id"`
		Security string `json:"security"`
	}{
		Level:    0,
		Email:    fmt.Sprintf("%s_%s", tag, subscription.Email),
		ID:       subscription.Passwd,
		Security: "auto",
	}
	return json.Marshal(&account)
}

func vlessUser(tag string, nodeInfo *api.RelayNodeInfo, subscription *api.SubscriptionInfo) (json.RawMessage, error) {
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}
	if subscription == nil {
		return nil, fmt.Errorf("subscription is nil")
	}
	account := struct {
		Level      int    `json:"level"`
		Email      string `json:"email"`
		Id         string `json:"id"`
		Flow       string `json:"flow"`
		Encryption string `json:"encryption"`
	}{
		Level:      0,
		Email:      fmt.Sprintf("%s_%s", tag, subscription.Email),
		Id:         subscription.Passwd,
		Flow:       nodeInfo.Flow,
		Encryption: nodeInfo.Encryption,
	}
	return json.Marshal(&account)
}
