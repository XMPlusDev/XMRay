package node

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	C "github.com/sagernet/sing/common"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/uuid"
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

	portRanges, err := parsePortString(nodeInfo.ListeningPort)
	if err != nil {
		return nil, fmt.Errorf("failed to parse listening port: %w", err)
	}
	if len(portRanges) == 0 {
		return nil, fmt.Errorf("no valid port ranges found in: %s", nodeInfo.ListeningPort)
	}

	inboundDetourConfig.PortList = &conf.PortList{Range: portRanges}
	inboundDetourConfig.Tag = tag
	inboundDetourConfig.SniffingConfig = &conf.SniffingConfig{
		Enabled:      nodeInfo.Sniffing,
		DestOverride: conf.StringList{"http", "tls", "quic", "fakedns"},
	}

	var proxySetting any

	switch nodeInfo.NodeType {
	case "vless":
		inboundDetourConfig.Protocol = "vless"
		if nodeInfo.Decryption == "none" {
			src := toFallBackConfigs(nodeInfo.FallbackConfigs)
			if len(src) > 0 {
				fallbacks, err := buildVlessFallbacks(src)
				if err != nil {
					return nil, err
				}
				proxySetting = &conf.VLessInboundConfig{Decryption: nodeInfo.Decryption, Fallbacks: fallbacks}
			} else {
				proxySetting = &conf.VLessInboundConfig{Decryption: nodeInfo.Decryption}
			}
		} else {
			proxySetting = &conf.VLessInboundConfig{Decryption: nodeInfo.Decryption}
		}

	case "vmess":
		inboundDetourConfig.Protocol = "vmess"
		proxySetting = &conf.VMessInboundConfig{}

	case "trojan":
		inboundDetourConfig.Protocol = "trojan"
		src := toFallBackConfigs(nodeInfo.FallbackConfigs)
		if len(src) > 0 {
			fallbacks, err := buildTrojanFallbacks(src)
			if err != nil {
				return nil, err
			}
			proxySetting = &conf.TrojanServerConfig{Fallbacks: fallbacks}
		} else {
			proxySetting = &conf.TrojanServerConfig{}
		}

	case "shadowsocks":
		inboundDetourConfig.Protocol = "shadowsocks"
		cipher := strings.ToLower(nodeInfo.Cipher)
		shadowsocksSetting := &conf.ShadowsocksServerConfig{
			Cipher:   cipher,
			Password: nodeInfo.ServerKey,
		}
		b := make([]byte, 32)
		rand.Read(b)
		if C.Contains(shadowaead_2022.List, cipher) {
			shadowsocksSetting.Users = append(shadowsocksSetting.Users, &conf.ShadowsocksUserConfig{
				Password: base64.StdEncoding.EncodeToString(b),
			})
		} else {
			randPasswd := uuid.New()
			shadowsocksSetting.Password = randPasswd.String()
		}
		shadowsocksSetting.NetworkList = &conf.NetworkList{"tcp", "udp"}
		proxySetting = shadowsocksSetting

	case "hysteria":
		inboundDetourConfig.Protocol = "hysteria"
		proxySetting = &conf.HysteriaServerConfig{
			Version: nodeInfo.HysteriaSettings.Version,
		}

	default:
		return nil, fmt.Errorf("unsupported node type: %v", nodeInfo.NodeType)
	}

	settingBytes, err := json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy %s config failed: %s", nodeInfo.NodeType, err)
	}
	setting := json.RawMessage(settingBytes)
	inboundDetourConfig.Settings = &setting

	streamSetting := new(conf.StreamConfig)
	transportProtocol := conf.TransportProtocol(nodeInfo.NetworkType)
	networkType, err := transportProtocol.Build()
	if err != nil {
		return nil, fmt.Errorf("convert TransportProtocol failed: %s", err)
	}
	streamSetting.Method = &transportProtocol

	switch networkType {
	case "hysteria", "hysteria2":
		hysteriaSettings := &conf.HysteriaConfig{}
		if nodeInfo.HysteriaSettings != nil {
			hysteriaSettings.Version = nodeInfo.HysteriaSettings.Version
		}
		streamSetting.HysteriaSettings = hysteriaSettings

	case "xhttp", "splithttp":
		if nodeInfo.XhttpSettings == nil {
			return nil, fmt.Errorf("XhttpSettings is required for xhttp transport")
		}
		x := nodeInfo.XhttpSettings
		xhttpSettings := &conf.SplitHTTPConfig{
			Host:               x.Host,
			Path:               x.Path,
			Mode:               x.Mode,
			NoSSEHeader:        x.NoSSEHeader,
			ScMaxBufferedPosts: x.ScMaxBufferedPosts,
		}
		if x.Mode == "packet-up" {
			xhttpSettings.ScMaxEachPostBytes = conf.Int32Range{From: x.ScMaxEachPostBytes, To: x.ScMaxEachPostBytes}
		}
		scStreamUpServerSecs, err := parseInt32Range(x.ScStreamUpServerSecs, 20, 80)
		if err != nil {
			return nil, fmt.Errorf("ScStreamUpServerSecs: %w", err)
		}
		xhttpSettings.ScStreamUpServerSecs = scStreamUpServerSecs
		xPaddingBytes, err := parseInt32Range(x.XPaddingBytes, 100, 1000)
		if err != nil {
			return nil, fmt.Errorf("XPaddingBytes: %w", err)
		}
		xhttpSettings.XPaddingBytes = xPaddingBytes
		if x.XPaddingObfsMode {
			xhttpSettings.XPaddingObfsMode = true
			xhttpSettings.XPaddingMethod = x.XPaddingMethod
			xhttpSettings.XPaddingPlacement = x.XPaddingPlacement
			xhttpSettings.XPaddingKey = x.XPaddingKey
			xhttpSettings.XPaddingHeader = x.XPaddingHeader
			xhttpSettings.UplinkHTTPMethod = x.UplinkHTTPMethod
			xhttpSettings.SessionIDPlacement = x.SessionIDPlacement
			xhttpSettings.SessionIDKey = x.SessionIDKey
			xhttpSettings.SessionIDTable = x.SessionIDTable
			xhttpSettings.SeqPlacement = x.SeqPlacement
			xhttpSettings.SeqKey = x.SeqKey
			xhttpSettings.UplinkDataPlacement = x.UplinkDataPlacement
			xhttpSettings.UplinkDataKey = x.UplinkDataKey

			if x.SessionIDLength != "" {
				sessionIDLength, err := parseInt32Range(x.SessionIDLength, 0, 0)
				if err != nil {
					return nil, fmt.Errorf("SessionIDLength: %w", err)
				}
				xhttpSettings.SessionIDLength = sessionIDLength
			}
			if x.UplinkChunkSize != "" {
				uplinkChunkSize, err := parseInt32Range(x.UplinkChunkSize, 0, 0)
				if err != nil {
					return nil, fmt.Errorf("UplinkChunkSize: %w", err)
				}
				xhttpSettings.UplinkChunkSize = uplinkChunkSize
			}
		}
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
		xhttpSettings.Xmux = conf.XmuxConfig{
			MaxConcurrency:   maxConcurrency,
			MaxConnections:   maxConnections,
			CMaxReuseTimes:   cMaxReuseTimes,
			HMaxRequestTimes: hMaxRequestTimes,
			HMaxReusableSecs: hMaxReusableSecs,
			HKeepAlivePeriod: x.Xmux.HKeepAlivePeriod,
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

	if nodeInfo.SecurityType == "tls" && nodeInfo.TlsSettings != nil && nodeInfo.TlsSettings.CertMode != "none" {
		streamSetting.Security = "tls"
		certFile, keyFile, err := getCertFile(config.CertConfig, nodeInfo.TlsSettings)
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
		if nodeInfo.TlsSettings.MinVersion != "" {
			tlsSettings.MinVersion = nodeInfo.TlsSettings.MinVersion
		}
		if nodeInfo.TlsSettings.MaxVersion != "" {
			tlsSettings.MaxVersion = nodeInfo.TlsSettings.MaxVersion
		}
		if nodeInfo.TlsSettings.CipherSuites != "" {
			tlsSettings.CipherSuites = nodeInfo.TlsSettings.CipherSuites
		}
		streamSetting.TLSSettings = tlsSettings
	}

	if nodeInfo.SecurityType == "reality" && nodeInfo.RealitySettings != nil {
		streamSetting.Security = "reality"
		rs := nodeInfo.RealitySettings
		realitySettings := &conf.REALITYConfig{
			Target:      rs.Dest,
			Show:        rs.Show,
			Xver:        rs.Xver,
			ServerNames: rs.ServerNames,
			PrivateKey:  rs.PrivateKey,
			ShortIds:    rs.ShortIds,
			Mldsa65Seed: rs.Mldsa65Seed,
		}
		if rs.MinClientVer != "" {
			realitySettings.MinClientVer = rs.MinClientVer
		}
		if rs.MaxClientVer != "" {
			realitySettings.MaxClientVer = rs.MaxClientVer
		}
		if rs.MaxTimeDiff > 0 {
			realitySettings.MaxTimeDiff = rs.MaxTimeDiff
		}
		streamSetting.REALITYSettings = realitySettings
	}

	if nodeInfo.SocketSettings != nil && nodeInfo.SocketSettings.Enabled {
		streamSetting.SocketSettings = buildSocketConfig(nodeInfo.SocketSettings, true)
	}

	inboundDetourConfig.StreamSetting = streamSetting
	return inboundDetourConfig.Build()
}

func getCertFile(certConfig *cert.CertConfig, tlsSettings *api.TlsSettings) (certFile string, keyFile string, err error) {
	if certConfig == nil {
		return "", "", fmt.Errorf("certConfig is nil")
	}
	switch tlsSettings.CertMode {
	case "file":
		cf := certConfig.CertFile
		kf := certConfig.KeyFile
		if tlsSettings != nil && tlsSettings.CertFile != "" {
			cf = tlsSettings.CertFile
		}
		if tlsSettings != nil && tlsSettings.KeyFile != "" {
			kf = tlsSettings.KeyFile
		}
		if cf == "" || kf == "" {
			return "", "", fmt.Errorf("cert file path or key file path missing")
		}
		return cf, kf, nil
	case "dns":
		pn := certConfig.Provider
		if tlsSettings != nil {
			pn = tlsSettings.DnsProvider
		}
		if pn == "" {
			return "", "", fmt.Errorf("cert dns provider name is required")
		}
		lego, err := cert.NewForNode(certConfig, pn)
		if err != nil {
			return "", "", err
		}
		return lego.DNSCert(tlsSettings.CertMode, tlsSettings.CertDomainName, tlsSettings.CertEmail)
	case "http", "tls":
		lego, err := cert.New(certConfig)
		if err != nil {
			return "", "", err
		}
		return lego.HTTPCert(tlsSettings.CertMode, tlsSettings.CertDomainName, tlsSettings.CertEmail)
	default:
		return "", "", fmt.Errorf("unsupported certmode: %s", tlsSettings.CertMode)
	}
}

// toFallBackConfigs converts api.FallbackConfig slice to node.FallBackConfig pointers.
func toFallBackConfigs(in []api.FallbackConfig) []*FallBackConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]*FallBackConfig, len(in))
	for i, f := range in {
		out[i] = &FallBackConfig{
			SNI:              f.SNI,
			Alpn:             f.Alpn,
			Path:             f.Path,
			Dest:             f.Dest,
			ProxyProtocolVer: f.ProxyProtocolVer,
		}
	}
	return out
}

func buildVlessFallbacks(fallbackConfigs []*FallBackConfig) ([]*conf.VLessInboundFallback, error) {
	if fallbackConfigs == nil {
		return nil, fmt.Errorf("you must provide FallBackConfigs")
	}
	vlessFallBacks := make([]*conf.VLessInboundFallback, len(fallbackConfigs))
	for i, c := range fallbackConfigs {
		if c.Dest == "" {
			return nil, fmt.Errorf("dest is required for fallback")
		}
		dest, err := json.Marshal(c.Dest)
		if err != nil {
			return nil, fmt.Errorf("marshal dest config failed: %s", err)
		}
		vlessFallBacks[i] = &conf.VLessInboundFallback{
			Name: c.SNI, Alpn: c.Alpn, Path: c.Path, Dest: dest, Xver: c.ProxyProtocolVer,
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
			return nil, fmt.Errorf("dest is required for fallback")
		}
		dest, err := json.Marshal(c.Dest)
		if err != nil {
			return nil, fmt.Errorf("marshal dest config failed: %s", err)
		}
		trojanFallBacks[i] = &conf.TrojanInboundFallback{
			Name: c.SNI, Alpn: c.Alpn, Path: c.Path, Dest: dest, Xver: c.ProxyProtocolVer,
		}
	}
	return trojanFallBacks, nil
}
