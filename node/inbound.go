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
	
	"github.com/xmplusdev/xmplus-server/api"
	"github.com/xmplusdev/xmplus-server/helper/cert"
)

func InboundBuilder(config *Config, nodeInfo *api.NodeInfo, tag string) (*core.InboundHandlerConfig, error) {
	// Add nil checks at the beginning
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if nodeInfo == nil {
		return nil, fmt.Errorf("nodeInfo is nil")
	}
	
	inboundDetourConfig := &conf.InboundDetourConfig{}
	
	if nodeInfo.NodeType == "Shadowsocks-Plugin" {
		inboundDetourConfig.ListenOn = &conf.Address{Address: net.ParseAddress("127.0.0.1")}
	} else if nodeInfo.ListeningIP != "" {
		ipAddress := net.ParseAddress(nodeInfo.ListeningIP)
		inboundDetourConfig.ListenOn = &conf.Address{Address: ipAddress}
	}
	
	// Parse port using the same function as router.go
	portRanges, err := parsePortString(nodeInfo.ListeningPort)
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
		DestOverride: &conf.StringList{"http", "tls", "quic", "fakedns"},
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
		case "shadowsocks", "Shadowsocks-Plugin":
			protocol = "shadowsocks"
			cipher := strings.ToLower(nodeInfo.Cipher)

			shadowsocksSetting := &conf.ShadowsocksServerConfig{
				Cipher:   cipher,
				Password: nodeInfo.ServerKey, // shadowsocks2022 shareKey
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
		case "dokodemo-door":
			protocol = "dokodemo-door"
			proxySetting = struct {
				Host        string   `json:"address"`
				NetworkList []string `json:"network"`
			}{
				Host:        "v1.mux.cool",
				NetworkList: []string{"tcp", "udp"},
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
	
	switch networkType {
		case "tcp", "raw":
			tcpSetting := &conf.TCPConfig{
				AcceptProxyProtocol: nodeInfo.AcceptProxyProtocol,
			}
			// Check if RawSettings is not nil before accessing Header
			if nodeInfo.RawSettings != nil {
				tcpSetting.HeaderConfig = nodeInfo.RawSettings.Header
			}
			streamSetting.TCPSettings = tcpSetting
		case "websocket":
			wsSettings := &conf.WebSocketConfig{
				AcceptProxyProtocol: nodeInfo.AcceptProxyProtocol,
			}
			// Check if WsSettings is not nil
			if nodeInfo.WsSettings != nil {
				wsSettings.Path = nodeInfo.WsSettings.Path
				wsSettings.Host = nodeInfo.WsSettings.Host
				wsSettings.HeartbeatPeriod = nodeInfo.WsSettings.HeartbeatPeriod
			}
			streamSetting.WSSettings = wsSettings	
		case "httpupgrade":
			httpupgradeSettings := &conf.HttpUpgradeConfig{
				AcceptProxyProtocol: nodeInfo.AcceptProxyProtocol,
			}
			// Check if HttpSettings is not nil
			if nodeInfo.HttpSettings != nil {
				httpupgradeSettings.Host = nodeInfo.HttpSettings.Host
				httpupgradeSettings.Path = nodeInfo.HttpSettings.Path
			}
			streamSetting.HTTPUPGRADESettings = httpupgradeSettings	
		case "xhttp":
			var ScStreamUpA, ScStreamUpB int32 = 20, 80
			
			// Check if XhttpSettings is not nil
			if nodeInfo.XhttpSettings == nil {
				return nil, fmt.Errorf("XhttpSettings is required for xhttp transport")
			}
			
			if strings.Contains(nodeInfo.XhttpSettings.ScStreamUpServerSecs, "-") {
				parts := strings.Split(nodeInfo.XhttpSettings.ScStreamUpServerSecs, "-")
				if len(parts) == 2 {
					parsedStream1, err := strconv.ParseInt(parts[0], 10, 32)
					if err != nil {
						return nil, err
					}
					ScStreamUpA = int32(parsedStream1)
					
					parsedStream2, err := strconv.ParseInt(parts[1], 10, 32)
					if err != nil {
						return nil, err
					}
					ScStreamUpB = int32(parsedStream2)
					
				}else{
					return nil, fmt.Errorf("invalid range format: %s", nodeInfo.XhttpSettings.ScStreamUpServerSecs)
				}
			} else {
				parsedStream, err := strconv.ParseInt(nodeInfo.XhttpSettings.ScStreamUpServerSecs, 10, 32)
				if err != nil {
					return nil, err
				}
				
				ScStreamUpA = int32(parsedStream)
				ScStreamUpB = int32(parsedStream)
			}
			
			scStreamUpServerSecs := conf.Int32Range{
				From: ScStreamUpA, 
				To: ScStreamUpB,
			}
			
			var XPaddingA, XPaddingB int32 = 100, 100
			if strings.Contains(nodeInfo.XhttpSettings.XPaddingBytes, "-") {
				parts := strings.Split(nodeInfo.XhttpSettings.XPaddingBytes, "-")
				if len(parts) == 2 {
					parsedXPadding1, err := strconv.ParseInt(parts[0], 10, 32)
					if err != nil {
						return nil, err
					}
					XPaddingA = int32(parsedXPadding1)
					
					parsedXPadding2, err := strconv.ParseInt(parts[1], 10, 32)
					if err != nil {
						return nil, err
					}
					XPaddingB = int32(parsedXPadding2)
					
				}else{
					return nil, fmt.Errorf("invalid range format: %s", nodeInfo.XhttpSettings.XPaddingBytes)
				}
			} else {
				parsedXPadding, err := strconv.ParseInt(nodeInfo.XhttpSettings.XPaddingBytes, 10, 32)
				if err != nil {
					return nil, err
				}
				
				XPaddingA = int32(parsedXPadding)
				XPaddingB = int32(parsedXPadding)
			}
			
			xPaddingBytes := conf.Int32Range{
				From: XPaddingA, 
				To: XPaddingB,
			}
			xhttpSettings := &conf.SplitHTTPConfig{
				Host: nodeInfo.XhttpSettings.Host,
				Path: nodeInfo.XhttpSettings.Path,
				Mode: nodeInfo.XhttpSettings.Mode,
				NoSSEHeader: nodeInfo.XhttpSettings.NoSSEHeader,
				ScMaxBufferedPosts: nodeInfo.XhttpSettings.ScMaxBufferedPosts,
				ScStreamUpServerSecs: scStreamUpServerSecs,
				XPaddingBytes: xPaddingBytes,
			}
			if(nodeInfo.XhttpSettings.Mode == "packet-up"){
				scMaxEachPostBytes := conf.Int32Range{
					From: nodeInfo.XhttpSettings.ScMaxEachPostBytes, 
					To: nodeInfo.XhttpSettings.ScMaxEachPostBytes,
				}
				xhttpSettings.ScMaxEachPostBytes = scMaxEachPostBytes
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
		case "mkcp":
			kcpSettings := &conf.KCPConfig{}
			// Check if KcpSettings is not nil
			if nodeInfo.KcpSettings != nil {
				kcpSettings.Congestion = &nodeInfo.KcpSettings.Congestion
				kcpSettings.Mtu = &nodeInfo.KcpSettings.Mtu
			}
			streamSetting.KCPSettings = kcpSettings	
	}
	
	streamSetting.Network = &transportProtocol	
	
	// FIXED: Check nil BEFORE accessing Enabled
	if nodeInfo.MaskSettings != nil && nodeInfo.MaskSettings.Enabled {
		mask := conf.Mask{
			Type:     nodeInfo.MaskSettings.Type,
			Settings: nodeInfo.MaskSettings.Settings,
		}
		
		finalMaskSettings := &conf.FinalMask{
			Udp: []conf.Mask{mask}, 
		}
		
		streamSetting.FinalMask = finalMaskSettings
	}
	
	// Check if TlsSettings is not nil before accessing its fields
	if nodeInfo.SecurityType == "tls" && nodeInfo.TlsSettings != nil && nodeInfo.TlsSettings.CertMode != "none" {
		streamSetting.Security = "tls"
		certFile, keyFile, err := getCertFile(config.CertConfig, nodeInfo.TlsSettings.CertMode, nodeInfo.TlsSettings.CertDomainName)
		if err != nil {
			return nil, err
		}
			
		tlsSettings := &conf.TLSConfig{}
		
		tlsSettings.Certs = append(tlsSettings.Certs, &conf.TLSCertConfig{CertFile: certFile, KeyFile: keyFile, OcspStapling: 3600})
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

	// Check if RealitySettings is not nil
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

	// Check if SocketSettings is not nil
	if nodeInfo.SocketSettings != nil && nodeInfo.SocketSettings.Enabled {
		sockoptConfig := &conf.SocketConfig{}
		if networkType != "tcp" && networkType != "ws" && nodeInfo.AcceptProxyProtocol {
			sockoptConfig.AcceptProxyProtocol = nodeInfo.AcceptProxyProtocol
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