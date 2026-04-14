package node

import (
	"encoding/json"
	"fmt"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/common/net"
	
	"github.com/xmplusdev/xmray/api"
)


func OutboundBuilder(config *Config, nodeInfo *api.NodeInfo, tag string) (*core.OutboundHandlerConfig, error) {
	// Add nil checks
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}
	
	outboundDetourConfig := &conf.OutboundDetourConfig{}
	
	outboundDetourConfig.Protocol = "freedom"
	outboundDetourConfig.Tag = tag

	// Build Send IP address
	if nodeInfo.SendThroughIP != "" {
		outboundDetourConfig.SendThrough = &nodeInfo.SendThroughIP
	}

	// Freedom Protocol setting
	var domainStrategy = "Asis"
	if config.EnableDNS {
		if config.DNSStrategy != "" {
			domainStrategy = config.DNSStrategy
		} else {
			domainStrategy = "Asis"
		}
	}
	proxySetting := &conf.FreedomConfig{
		DomainStrategy: domainStrategy,
	}
	
	var setting json.RawMessage
	setting, err := json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy %s config fialed: %s", nodeInfo.NodeType, err)
	}
	
	outboundDetourConfig.Settings = &setting
	return outboundDetourConfig.Build()	
}

func BlackholeOutboundBuilder(tag string) (*core.OutboundHandlerConfig, error) {
	outboundDetourConfig := &conf.OutboundDetourConfig{}
	
	outboundDetourConfig.Protocol = "blackhole"
	outboundDetourConfig.Tag = fmt.Sprintf("%s_blackhole", tag)
	
	return outboundDetourConfig.Build()	
}


func OutboundRelayBuilder(nodeInfo *api.RelayNodeInfo, tag string, subscription *api.SubscriptionInfo, Passwd string) (*core.OutboundHandlerConfig, error) {
	// Add nil checks
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}
	if subscription == nil {
		return nil, fmt.Errorf("subscription is nil")
	}
	
	outboundDetourConfig := &conf.OutboundDetourConfig{}
	
	var (
		protocol      string
		streamSetting *conf.StreamConfig
		setting       json.RawMessage
	)

	var proxySetting any
	
	switch nodeInfo.NodeType {
		case "vless":
			protocol = "vless"
			vUser, err := vlessUser(tag, nodeInfo, subscription)
			if err != nil {
				return nil, fmt.Errorf("Marshal Vless User config failed: %s", err)
			}
			User := []json.RawMessage{vUser}
			
			proxySetting = struct {
				Vnext []*conf.VLessOutboundVnext `json:"vnext"`
			}{
				Vnext: []*conf.VLessOutboundVnext{{
					Address: &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
					Port:    uint16(nodeInfo.ListeningPort),
					Users:   User,
				}},
			}
		case "vmess":
			protocol = "vmess"		
			userVmess, err := vmessUser(tag, subscription)
			if err != nil {
				return nil, fmt.Errorf("Marshal Vmess User config failed: %s", err)
			}
			User := []json.RawMessage{userVmess}
			
			proxySetting = struct {
				Receivers []*conf.VMessOutboundTarget `json:"vnext"`
			}{
				Receivers: []*conf.VMessOutboundTarget{{
					Address: &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
					Port:    uint16(nodeInfo.ListeningPort),
					Users:   User,
				}},
			}
		case "trojan":
			protocol = "trojan"	
			proxySetting = struct {
				Servers []*conf.TrojanServerTarget `json:"servers"`
			}{
				Servers: []*conf.TrojanServerTarget{&conf.TrojanServerTarget{
						Address: &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
						Port:    uint16(nodeInfo.ListeningPort),
						Password: subscription.Passwd,
						Email:  fmt.Sprintf("%s|%s|%d", tag, subscription.Email, subscription.Id),
						Level:  0,
						Flow: "",
					},
				},
			}
		case "Shadowsocks":
			protocol = "shadowsocks"
			proxySetting = struct {
				Servers []*conf.ShadowsocksServerTarget `json:"servers"`
			}{
				Servers: []*conf.ShadowsocksServerTarget{&conf.ShadowsocksServerTarget{
						Address: &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
						Port:    uint16(nodeInfo.ListeningPort),
						Password: Passwd,
						Email:   fmt.Sprintf("%s|%s|%d", tag, subscription.Email, subscription.Id),
						Level:   0,
						Cipher:  nodeInfo.Cipher,
						UoT:     true,
					},
				},
			}
		case "hysteria":	
		    protocol = "hysteria" 
			proxySetting = struct {
				*conf.HysteriaClientConfig
			}{
				&conf.HysteriaClientConfig {
					Address: &conf.Address{Address: net.ParseAddress(nodeInfo.Address)},
					Port:    uint16(nodeInfo.ListeningPort),
					Version: nodeInfo.HysteriaSettings.Version,
				},
			}
		default:
			return nil, fmt.Errorf("Unsupported Relay Node Type: %s", nodeInfo.NodeType)		
	}
	
	setting, err := json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy %s config fialed: %s", nodeInfo.NodeType, err)
	}
	
	outboundDetourConfig.Protocol = protocol
	outboundDetourConfig.Settings = &setting
	
	streamSetting = new(conf.StreamConfig)
	transportProtocol := conf.TransportProtocol(nodeInfo.NetworkType)
	networkType, err := transportProtocol.Build()
	if err != nil {
		return nil, fmt.Errorf("convert TransportProtocol failed: %s", err)
	}
	
	streamSetting.Network = &transportProtocol
	
	switch networkType {
	case "tcp", "raw":
		tcpSetting := &conf.TCPConfig{}
		// Check if RawSettings is not nil
		if nodeInfo.RawSettings != nil {
			tcpSetting.HeaderConfig = nodeInfo.RawSettings.Header
			tcpSetting.AcceptProxyProtocol = nodeInfo.RawSettings.AcceptProxyProtocol
		}
		streamSetting.TCPSettings = tcpSetting
	case "websocket", "ws":
		wsSettings := &conf.WebSocketConfig{}
		// Check if WsSettings is not nil
		if nodeInfo.WsSettings != nil {
			wsSettings.Path = nodeInfo.WsSettings.Path
			wsSettings.Host = nodeInfo.WsSettings.Host
			wsSettings.HeartbeatPeriod = nodeInfo.WsSettings.HeartbeatPeriod
			wsSettings.AcceptProxyProtocol = nodeInfo.WsSettings.AcceptProxyProtocol
		}
		streamSetting.WSSettings = wsSettings
	case "httpupgrade":
		httpupgradeSettings := &conf.HttpUpgradeConfig{}
		// Check if HttpSettings is not nil
		if nodeInfo.HttpSettings != nil {
			httpupgradeSettings.AcceptProxyProtocol = nodeInfo.HttpSettings.AcceptProxyProtocol
			httpupgradeSettings.Host = nodeInfo.HttpSettings.Host
			httpupgradeSettings.Path = nodeInfo.HttpSettings.Path
		}
		streamSetting.HTTPUPGRADESettings = httpupgradeSettings	
	case "xhttp", "splithttp":
		xhttpSettings := &conf.SplitHTTPConfig{}
		// Check if XhttpSettings is not nil
		if nodeInfo.XhttpSettings != nil {
			xhttpSettings.Host = nodeInfo.XhttpSettings.Host
			xhttpSettings.Path = nodeInfo.XhttpSettings.Path
			xhttpSettings.Mode = nodeInfo.XhttpSettings.Mode
		}
		streamSetting.XHTTPSettings = xhttpSettings		
	case "grpc":
		grpcSettings := &conf.GRPCConfig{}
		// Check if GrpcSettings is not nil
		if nodeInfo.GrpcSettings != nil {
			grpcSettings.ServiceName = nodeInfo.GrpcSettings.ServiceName
			grpcSettings.Authority = nodeInfo.GrpcSettings.Authority
			grpcSettings.InitialWindowsSize = nodeInfo.GrpcSettings.WindowsSize
			grpcSettings.UserAgent = nodeInfo.GrpcSettings.UserAgent
			grpcSettings.IdleTimeout = nodeInfo.GrpcSettings.IdleTimeout
			grpcSettings.HealthCheckTimeout = nodeInfo.GrpcSettings.HealthCheckTimeout
			grpcSettings.PermitWithoutStream = nodeInfo.GrpcSettings.PermitWithoutStream
		}
		streamSetting.GRPCSettings = grpcSettings
	case "mkcp", "kcp":
		kcpSettings := &conf.KCPConfig{}
		// Check if KcpSettings is not nil
		if nodeInfo.KcpSettings != nil {
			kcpSettings.Mtu = &nodeInfo.KcpSettings.Mtu
		}
		streamSetting.KCPSettings = kcpSettings	
	
	case "hysteria", "hysteria2":	
		hysteriaSettings := &conf.HysteriaConfig{}
		// Check if hysteriaSettings is not nil
		if nodeInfo.HysteriaSettings != nil {
			hysteriaSettings.Version = nodeInfo.HysteriaSettings.Version
			hysteriaSettings.Auth = subscription.Passwd
		}
			
		streamSetting.HysteriaSettings = hysteriaSettings
	}
	
	if nodeInfo.MaskSettings != nil && nodeInfo.MaskSettings.Enabled {
		finalMaskSettings := &conf.FinalMask{}

		if nodeInfo.MaskSettings.UDP != nil {
			udpMask := conf.Mask{Type: nodeInfo.MaskSettings.UDP.Type}
			if nodeInfo.MaskSettings.UDP.Settings != nil {
				udpMask.Settings = nodeInfo.MaskSettings.UDP.Settings
			}
			finalMaskSettings.Udp = []conf.Mask{udpMask}
		}

		if nodeInfo.MaskSettings.TCP != nil {
			tcpMask := conf.Mask{Type: nodeInfo.MaskSettings.TCP.Type}
			if nodeInfo.MaskSettings.TCP.Settings != nil {
				tcpMask.Settings = nodeInfo.MaskSettings.TCP.Settings
			}
			finalMaskSettings.Tcp = []conf.Mask{tcpMask}
		}

		if nodeInfo.MaskSettings.QuicParams != nil && nodeInfo.MaskSettings.EnabledQuic {
			finalMaskSettings.QuicParams = buildQuicParams(nodeInfo.MaskSettings.QuicParams)
		}

		streamSetting.FinalMask = finalMaskSettings
	}
	
	// Check if TlsSettings is not nil
	if nodeInfo.SecurityType == "tls" && nodeInfo.TlsSettings != nil {
		streamSetting.Security = "tls"
		tlsSettings := &conf.TLSConfig{
			AllowInsecure: nodeInfo.TlsSettings.AllowInsecure,
			ServerName: nodeInfo.TlsSettings.ServerName,
			Fingerprint: nodeInfo.TlsSettings.FingerPrint,
			VerifyPeerCertByName: nodeInfo.TlsSettings.VerifyPeerCertByName,
			ECHConfigList: nodeInfo.TlsSettings.ECHConfigList,
			PinnedPeerCertSha256: nodeInfo.TlsSettings.PinnedPeerCertSha256,
		}
		streamSetting.TLSSettings = tlsSettings	
	}
	
	// Check if RealitySettings is not nil
	if nodeInfo.SecurityType == "reality" && nodeInfo.RealitySettings != nil {
		streamSetting.Security = "reality"		
		realitySettings :=  &conf.REALITYConfig{
			Show:         nodeInfo.RealitySettings.Show,
			ServerName:   nodeInfo.RealitySettings.ServerName,
			PublicKey:    nodeInfo.RealitySettings.PublicKey,
			Fingerprint:  nodeInfo.RealitySettings.Fingerprint,
			ShortId:      nodeInfo.RealitySettings.ShortId,
			SpiderX:      nodeInfo.RealitySettings.SpiderX,
			Mldsa65Verify: nodeInfo.RealitySettings.Mldsa65Verify,
		}
		streamSetting.REALITYSettings = realitySettings
	}
	
	outboundDetourConfig.Tag = fmt.Sprintf("%s_%d", tag, subscription.Id)
	if nodeInfo.SendThroughIP != "" {
		outboundDetourConfig.SendThrough = &nodeInfo.SendThroughIP
	}
	outboundDetourConfig.StreamSetting = streamSetting
	
	return outboundDetourConfig.Build()
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
		Email:    fmt.Sprintf("%s|%s|%d", tag, subscription.Email, subscription.Id),
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
		Level   int    `json:"level"`
		Email   string `json:"email"`
		Id      string `json:"id"`
		Flow    string `json:"flow"`
		Encryption string `json:"encryption"`
	}{
		Level:      0,
		Email:      fmt.Sprintf("%s|%s|%d", tag, subscription.Email, subscription.Id),
		Id:         subscription.Passwd,
		Flow:       nodeInfo.Flow,
		Encryption: nodeInfo.Encryption,
	}
	
	return json.Marshal(&account)
}