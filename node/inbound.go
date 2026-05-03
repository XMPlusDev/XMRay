package node

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"strconv"

	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	C "github.com/sagernet/sing/common"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	
	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/helper/cert"
)

func InboundBuilder(config *Config, nodeInfo *api.NodeInfo, tag string) (*core.InboundHandlerConfig, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}
	
	inboundDetourConfig := &conf.InboundDetourConfig{}
	
	if nodeInfo.ListeningIP != "" {
		ipAddress := net.ParseAddress(nodeInfo.ListeningIP)
		inboundDetourConfig.ListenOn = &conf.Address{Address: ipAddress}
	}
	
	portRanges, err := ParsePortString(nodeInfo.ListeningPort)
	if err != nil {
		return nil, fmt.Errorf("failed to parse listening port: %w", err)
	}
	
	if len(portRanges) == 0 {
		return nil, fmt.Errorf("no valid port ranges found in: %s", nodeInfo.ListeningPort)
	}
	
	portList := &conf.PortList{
		Range: portRanges,
	}

	inboundDetourConfig.PortList = portList
	inboundDetourConfig.Tag = tag

	sniffingConfig := &conf.SniffingConfig{
		Enabled:      nodeInfo.Sniffing,
		DestOverride: conf.StringList{"http", "tls", "quic", "fakedns"},
	}
	
	inboundDetourConfig.SniffingConfig = sniffingConfig
	
	var (
		protocol      string
		streamSetting *conf.StreamConfig
		setting       json.RawMessage
	)

	var proxySetting any
	
	switch nodeInfo.NodeType {
		case "vless":
			protocol = "vless"
			if nodeInfo.Decryption == "none" && config.EnableFallback {
				fallbackConfigs, err := buildVlessFallbacks(config.FallBackConfigs)
				if err == nil {
					proxySetting = &conf.VLessInboundConfig{
						Decryption: nodeInfo.Decryption,
						Fallbacks:  fallbackConfigs,
					} 
				}else {
					return nil, err
				}
			} else {
				proxySetting = &conf.VLessInboundConfig{
					Decryption: nodeInfo.Decryption,
				}
			}
		case "vmess":	
			protocol = "vmess"
			proxySetting = &conf.VMessInboundConfig{}

		case "trojan":
			protocol = "trojan"
			if config.EnableFallback {
				fallbackConfigs, err := buildTrojanFallbacks(config.FallBackConfigs)
				if err == nil {
					proxySetting = &conf.TrojanServerConfig{
						Fallbacks: fallbackConfigs,
					}
				}else {
					return nil, err
				}
			} else {
				proxySetting = &conf.TrojanServerConfig{}
			}
		case "shadowsocks":
			protocol = "shadowsocks"
			cipher := strings.ToLower(nodeInfo.Cipher)

			shadowsocksSetting := &conf.ShadowsocksServerConfig{
				Cipher:   cipher,
				Password: nodeInfo.ServerKey, 
			}

			b := make([]byte, 32)
			rand.Read(b)
			randPasswd := hex.EncodeToString(b)
			if C.Contains(shadowaead_2022.List, cipher) {
				shadowsocksSetting.Users = append(shadowsocksSetting.Users, &conf.ShadowsocksUserConfig{
					Password: base64.StdEncoding.EncodeToString(b),
				})
			} else {
				shadowsocksSetting.Password = randPasswd
			}

			shadowsocksSetting.NetworkList = &conf.NetworkList{"tcp", "udp"}
			
			proxySetting = shadowsocksSetting
		case "hysteria":	
			protocol = "hysteria" 
			proxySetting = &conf.HysteriaServerConfig{
				Version: nodeInfo.HysteriaSettings.Version,
			}
		default:
			return nil, fmt.Errorf("Unsupported Node Type: %v", nodeInfo.NodeType)	
	}
	
	setting, err = json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy %s config fialed: %s", nodeInfo.NodeType, err)
	}
	inboundDetourConfig.Protocol = protocol
	inboundDetourConfig.Settings = &setting
	
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
			if nodeInfo.RawSettings != nil {
				tcpSetting.HeaderConfig = nodeInfo.RawSettings.Header
				tcpSetting.AcceptProxyProtocol = nodeInfo.RawSettings.AcceptProxyProtocol
			}
			streamSetting.TCPSettings = tcpSetting
		case "websocket", "ws":
			wsSettings := &conf.WebSocketConfig{}
			if nodeInfo.WsSettings != nil {
				wsSettings.Path = nodeInfo.WsSettings.Path
				wsSettings.Host = nodeInfo.WsSettings.Host
				wsSettings.HeartbeatPeriod = nodeInfo.WsSettings.HeartbeatPeriod
				wsSettings.AcceptProxyProtocol = nodeInfo.WsSettings.AcceptProxyProtocol
			}
			streamSetting.WSSettings = wsSettings	
		case "httpupgrade":
			httpupgradeSettings := &conf.HttpUpgradeConfig{}
			if nodeInfo.HttpSettings != nil {
				httpupgradeSettings.AcceptProxyProtocol = nodeInfo.HttpSettings.AcceptProxyProtocol
				httpupgradeSettings.Host = nodeInfo.HttpSettings.Host
				httpupgradeSettings.Path = nodeInfo.HttpSettings.Path
			}
			streamSetting.HTTPUPGRADESettings = httpupgradeSettings	
		case "xhttp", "splithttp":
			if nodeInfo.XhttpSettings == nil {
				return nil, fmt.Errorf("XhttpSettings is required for xhttp transport")
			}
			
			xhttpSettings := &conf.SplitHTTPConfig{
				Host: nodeInfo.XhttpSettings.Host,
				Path: nodeInfo.XhttpSettings.Path,
				Mode: nodeInfo.XhttpSettings.Mode,
				NoSSEHeader: nodeInfo.XhttpSettings.NoSSEHeader,
				ScMaxBufferedPosts: nodeInfo.XhttpSettings.ScMaxBufferedPosts,
			}

			if(nodeInfo.XhttpSettings.Mode == "packet-up"){
				scMaxEachPostBytes := conf.Int32Range{
					From: nodeInfo.XhttpSettings.ScMaxEachPostBytes, 
					To: nodeInfo.XhttpSettings.ScMaxEachPostBytes,
				}
				xhttpSettings.ScMaxEachPostBytes = scMaxEachPostBytes
			}
			
			scStreamUpServerSecs, err := parseInt32Range(nodeInfo.XhttpSettings.ScStreamUpServerSecs, 20, 80)
			if err != nil {
				return nil, fmt.Errorf("ScStreamUpServerSecs: %w", err)
			}
			xhttpSettings.ScStreamUpServerSecs = scStreamUpServerSecs

			xPaddingBytes, err := parseInt32Range(nodeInfo.XhttpSettings.XPaddingBytes, 100, 100)
			if err != nil {
				return nil, fmt.Errorf("XPaddingBytes: %w", err)
			}
			xhttpSettings.XPaddingBytes = xPaddingBytes
			
			streamSetting.XHTTPSettings = xhttpSettings		
		case "grpc":
			grpcSettings := &conf.GRPCConfig{}
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
			if nodeInfo.KcpSettings != nil {
				kcpSettings.Mtu = &nodeInfo.KcpSettings.Mtu
			}
			streamSetting.KCPSettings = kcpSettings	
		case "hysteria", "hysteria2":	
			hysteriaSettings := &conf.HysteriaConfig{}
			if nodeInfo.HysteriaSettings != nil {
				hysteriaSettings.Version = nodeInfo.HysteriaSettings.Version
			}
			
			streamSetting.HysteriaSettings = hysteriaSettings
		default:
			return nil, fmt.Errorf("Unsupported transport protocol: %v", networkType)	
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
	
	if nodeInfo.SecurityType == "tls" && nodeInfo.TlsSettings != nil && nodeInfo.TlsSettings.CertMode != "none" {
		streamSetting.Security = "tls"
		certFile, keyFile, err := getCertFile(config.CertConfig, nodeInfo.TlsSettings.CertMode, nodeInfo.TlsSettings.CertDomainName)
		if err != nil {
			return nil, err
		}
			
		tlsSettings := &conf.TLSConfig{}
		
		tlsSettings.Certs = append(tlsSettings.Certs, &conf.TLSCertConfig{CertFile: certFile, KeyFile: keyFile})
		tlsSettings.RejectUnknownSNI = nodeInfo.TlsSettings.RejectUnknownSni
		tlsSettings.ServerName = nodeInfo.TlsSettings.ServerName
		
		alpn := conf.StringList(nodeInfo.TlsSettings.Alpn)
		tlsSettings.ALPN = &alpn
		
		curvePreferences := conf.StringList(nodeInfo.TlsSettings.CurvePreferences)
		tlsSettings.CurvePreferences = &curvePreferences
		
		tlsSettings.Fingerprint = nodeInfo.TlsSettings.FingerPrint
		tlsSettings.ECHServerKeys = nodeInfo.TlsSettings.ECHServerKeys

		streamSetting.TLSSettings = tlsSettings
	}

	if nodeInfo.SecurityType == "reality" && nodeInfo.RealitySettings != nil {
		streamSetting.Security = "reality"
		
		realitySettings :=  &conf.REALITYConfig{}
		
		realitySettings.Target = nodeInfo.RealitySettings.Dest
		realitySettings.Show = nodeInfo.RealitySettings.Show
		realitySettings.Xver = nodeInfo.RealitySettings.Xver
		realitySettings.ServerNames = nodeInfo.RealitySettings.ServerNames
		realitySettings.PrivateKey = nodeInfo.RealitySettings.PrivateKey
		realitySettings.ShortIds = nodeInfo.RealitySettings.ShortIds
		if nodeInfo.RealitySettings.MinClientVer != "" {
			realitySettings.MinClientVer = nodeInfo.RealitySettings.MinClientVer
		}
		if nodeInfo.RealitySettings.MaxClientVer != "" {
			realitySettings.MaxClientVer = nodeInfo.RealitySettings.MaxClientVer
		}	
		if nodeInfo.RealitySettings.MaxTimeDiff > 0 {
			realitySettings.MaxTimeDiff = nodeInfo.RealitySettings.MaxTimeDiff
		}
		realitySettings.Mldsa65Seed = nodeInfo.RealitySettings.Mldsa65Seed
		
		streamSetting.REALITYSettings = realitySettings
	}

	if nodeInfo.SocketSettings != nil && nodeInfo.SocketSettings.Enabled {
		sockoptConfig := &conf.SocketConfig{}
		
		if  nodeInfo.SocketSettings.AcceptProxyProtocol {
			switch 	networkType{
				case "kcp", "xhttp", "grpc":
					sockoptConfig.AcceptProxyProtocol = nodeInfo.SocketSettings.AcceptProxyProtocol
			}
		}
		if nodeInfo.SocketSettings.DomainStrategy != "" {
			sockoptConfig.DomainStrategy = nodeInfo.SocketSettings.DomainStrategy
		}
		if nodeInfo.SocketSettings.TCPKeepAliveInterval > 0 {
			sockoptConfig.TCPKeepAliveInterval = nodeInfo.SocketSettings.TCPKeepAliveInterval
		}
		if nodeInfo.SocketSettings.TCPWindowClamp > 0 {
			sockoptConfig.TCPWindowClamp = nodeInfo.SocketSettings.TCPWindowClamp
		}
		if nodeInfo.SocketSettings.TCPMaxSeg > 0 {
			sockoptConfig.TCPMaxSeg = nodeInfo.SocketSettings.TCPMaxSeg
		}
		if nodeInfo.SocketSettings.TCPUserTimeout > 0 {
			sockoptConfig.TCPUserTimeout = nodeInfo.SocketSettings.TCPUserTimeout
		}
		if nodeInfo.SocketSettings.TCPKeepAliveIdle > 0 {
			sockoptConfig.TCPKeepAliveIdle = nodeInfo.SocketSettings.TCPKeepAliveIdle
		}
		if nodeInfo.SocketSettings.TcpMptcp {
			sockoptConfig.TcpMptcp = nodeInfo.SocketSettings.TcpMptcp
		}
		
		if nodeInfo.SocketSettings.TcpCongestion != "" {
			sockoptConfig.TCPCongestion = nodeInfo.SocketSettings.TcpCongestion
		}
			
		streamSetting.SocketSettings = sockoptConfig
	}	
	
	inboundDetourConfig.StreamSetting = streamSetting

	return inboundDetourConfig.Build()
}

func getCertFile(certConfig *cert.CertConfig, CertMode string, Domain string) (certFile string, keyFile string, err error) {
	if certConfig == nil {
		return "", "", fmt.Errorf("certConfig is nil")
	}
	
	switch CertMode {
	case "file":
		if certConfig.CertFile == "" || certConfig.KeyFile == "" {
			return "", "", fmt.Errorf("Cert file path or key file path missing, check your config.yml parameters.")
		}
		return certConfig.CertFile, certConfig.KeyFile, nil
	case "dns":
		lego, err := cert.New(certConfig)
		if err != nil {
			return "", "", err
		}
		certPath, keyPath, err := lego.DNSCert(CertMode, Domain)
		if err != nil {
			return "", "", err
		}
		return certPath, keyPath, err
	case "http", "tls":
		lego, err := cert.New(certConfig)
		if err != nil {
			return "", "", err
		}
		certPath, keyPath, err := lego.HTTPCert(CertMode, Domain)
		if err != nil {
			return "", "", err
		}
		return certPath, keyPath, err
	default:
		return "", "", fmt.Errorf("unsupported certmode: %s", CertMode)
	}
}

func buildVlessFallbacks(fallbackConfigs []*FallBackConfig) ([]*conf.VLessInboundFallback, error) {
	if fallbackConfigs == nil {
		return nil, fmt.Errorf("you must provide FallBackConfigs")
	}

	vlessFallBacks := make([]*conf.VLessInboundFallback, len(fallbackConfigs))
	for i, c := range fallbackConfigs {

		if c.Dest == "" {
			return nil, fmt.Errorf("dest is required for fallback fialed")
		}

		var dest json.RawMessage
		dest, err := json.Marshal(c.Dest)
		if err != nil {
			return nil, fmt.Errorf("marshal dest %s config fialed: %s", dest, err)
		}
		vlessFallBacks[i] = &conf.VLessInboundFallback{
			Name: c.SNI,
			Alpn: c.Alpn,
			Path: c.Path,
			Dest: dest,
			Xver: c.ProxyProtocolVer,
		}
	}
	return vlessFallBacks, nil
}

func buildTrojanFallbacks(fallbackConfigs []*FallBackConfig) ([]*conf.TrojanInboundFallback, error) {
	if fallbackConfigs == nil {
		return nil, fmt.Errorf("you must provide FallBackConfigs")
	}

	trojanFallBacks := make([]*conf.TrojanInboundFallback, len(fallbackConfigs))
	for i, c := range fallbackConfigs {

		if c.Dest == "" {
			return nil, fmt.Errorf("dest is required for fallback fialed")
		}

		var dest json.RawMessage
		dest, err := json.Marshal(c.Dest)
		if err != nil {
			return nil, fmt.Errorf("marshal dest %s config fialed: %s", dest, err)
		}
		trojanFallBacks[i] = &conf.TrojanInboundFallback{
			Name: c.SNI,
			Alpn: c.Alpn,
			Path: c.Path,
			Dest: dest,
			Xver: c.ProxyProtocolVer,
		}
	}
	return trojanFallBacks, nil
}

func parseInt32Range(s string, defaultA, defaultB int32) (conf.Int32Range, error) {
	if s == "" {
		return conf.Int32Range{From: defaultA, To: defaultB}, nil
	}

	if strings.Contains(s, "-") {
		parts := strings.Split(s, "-")
		if len(parts) != 2 {
			return conf.Int32Range{}, fmt.Errorf("invalid range format: %s", s)
		}
		a, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 32)
		if err != nil {
			return conf.Int32Range{}, fmt.Errorf("invalid range start %q: %w", parts[0], err)
		}
		b, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 32)
		if err != nil {
			return conf.Int32Range{}, fmt.Errorf("invalid range end %q: %w", parts[1], err)
		}
		return conf.Int32Range{From: int32(a), To: int32(b)}, nil
	}

	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return conf.Int32Range{}, fmt.Errorf("invalid value %q: %w", s, err)
	}
	return conf.Int32Range{From: int32(v), To: int32(v)}, nil
}