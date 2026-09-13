package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/bitly/go-simplejson"
	"github.com/xtls/xray-core/infra/conf"
)

func (c *Client) GetServerNodes() (*ServerNodesResponse, error) {
	res, err := c.client.R().
		SetBody(map[string]string{"key": c.Key, "core": "xray"}).
		ForceContentType("application/json").
		SetPathParam("serverId", strconv.Itoa(c.ServerID)).
		Post("/api/server/nodes/{serverId}")

	response, err := c.checkResponse(res, err)
	if err != nil {
		return nil, err
	}

	nodesJSON, err := response.Get("nodes").MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to parse nodes list: %w", err)
	}

	var nodes []*ServerNode
	if err := json.Unmarshal(nodesJSON, &nodes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nodes: %w", err)
	}

	pollInterval := response.Get("poll_interval").MustInt()
	version := response.Get("api_version").MustInt()

	return &ServerNodesResponse{
		Nodes:        nodes,
		PollInterval: pollInterval,
		Version:      version,
	}, nil
}

func (c *Client) ReportServerStatus(status *ServerStatus) error {
	postData := &PostData{Key: c.Key, Data: status}
	res, err := c.client.R().
		SetBody(postData).
		SetPathParam("serverId", strconv.Itoa(c.ServerID)).
		ForceContentType("application/json").
		Post("/api/server/status/{serverId}")

	_, err = c.checkResponse(res, err)
	return err
}

func (c *Client) GetNodeInfo() (*NodeInfo, error) {
	server := new(serverConfig)
	res, err := c.client.R().
		SetBody(map[string]string{"key": c.Key, "core": "xray"}).
		ForceContentType("application/json").
		SetPathParam("serverId", strconv.Itoa(c.NodeID)).
		SetHeader("If-None-Match", c.eTags["server"]).
		Post("/api/server/info/{serverId}")

	if err != nil {
		return nil, err
	}

	if res.StatusCode() == 304 {
		return nil, errors.New(NodeNotModified)
	}

	if res.Header().Get("Etag") != "" && res.Header().Get("Etag") != c.eTags["server"] {
		c.eTags["server"] = res.Header().Get("Etag")
	}

	response, err := c.checkResponse(res, err)
	if err != nil {
		return nil, err
	}

	b, _ := response.Encode()
	json.Unmarshal(b, server)

	if server.Type == "" {
		return nil, fmt.Errorf("server Type cannot be empty")
	}

	c.resp.Store(server)

	nodeInfo, err := c.NodeResponse(server)
	if err != nil {
		return nil, fmt.Errorf("parse node info failed: %s, error: %v", res.String(), err)
	}

	return nodeInfo, nil
}

func (c *Client) NodeResponse(s *serverConfig) (*NodeInfo, error) {
	nodeInfo := &NodeInfo{}

	transport, err := s.NetworkSettings.MarshalJSON()
	if err != nil {
		return nil, err
	}

	transportData, err := simplejson.NewJson(transport)
	if err != nil {
		return nil, err
	}

	nodeInfo.NodeType = strings.ToLower(s.Type)
	nodeInfo.NodeID = c.NodeID
	nodeInfo.RelayNodeID = int(s.RelayNodeId)
	nodeInfo.RelayType = int(s.RelayType)
	nodeInfo.SpeedLimit = uint64(s.Speedlimit * 1000000 / 8)
	nodeInfo.IgnoreIPs = s.IgnoreIPs
	nodeInfo.UpdateTime = int(s.UpdateInterval)
	nodeInfo.Sniffing = transportData.Get("sniffing").MustBool()
	nodeInfo.ListeningIP = transportData.Get("listeningIP").MustString()
	nodeInfo.ListeningPort = transportData.Get("listeningPort").MustString()
	nodeInfo.SendThroughIP = transportData.Get("sendThroughIP").MustString()

	if nodeInfo.NodeType == "vless" {
		nodeInfo.Decryption = transportData.Get("decryption").MustString()
		if _, flowExists := transportData.CheckGet("flow"); flowExists {
			nodeInfo.Flow = transportData.Get("flow").MustString()
		}
	}

	if nodeInfo.NodeType == "shadowsocks" {
		nodeInfo.Cipher = transportData.Get("cipher").MustString()
		nodeInfo.ServerKey = s.ServerKey
	}

	if err := parseNetworkSettings(transportData, &nodeInfo.NetworkType, &nodeInfo.HysteriaSettings,
		&nodeInfo.XhttpSettings, &nodeInfo.RawSettings, &nodeInfo.KcpSettings,
		&nodeInfo.GrpcSettings, &nodeInfo.WsSettings, &nodeInfo.HttpSettings); err != nil {
		return nil, err
	}

	security, err := s.SecuritySettings.MarshalJSON()
	if err != nil {
		return nil, err
	}

	securityData, err := simplejson.NewJson(security)
	if err != nil {
		return nil, err
	}

	if err := parseSecuritySettings(securityData, &nodeInfo.SecurityType, &nodeInfo.TlsSettings,
		&nodeInfo.RealitySettings, &nodeInfo.FinalRules); err != nil {
		return nil, err
	}

	if maskSettings, ok := securityData.CheckGet("maskSettings"); ok {
		if err := parseMaskSettingsInto(maskSettings, &nodeInfo.MaskSettings); err != nil {
			return nil, err
		}
	}

	if socketSettings, ok := securityData.CheckGet("socketSettings"); ok {
		sock := &SocketSettings{}
		fillSocketSettings(socketSettings, sock)
		nodeInfo.SocketSettings = sock
	}

	rule, err := s.Rules.MarshalJSON()
	if err != nil {
		return nil, err
	}

	ruleData, err := simplejson.NewJson(rule)
	if err != nil {
		return nil, err
	}

	nodeInfo.BlockingRules = parseBlockingRules(ruleData)

	if nodeInfo.NodeType == "vless" || nodeInfo.NodeType == "trojan" {
		nodeInfo.FallbackConfigs = parseFallbackConfigs(transportData)
	}

	return nodeInfo, nil
}

func (c *Client) GetTransitNode() (*RelayNodeInfo, error) {
	s := c.resp.Load().(*serverConfig)
	nodeInfo := &RelayNodeInfo{}

	transport, err := s.RNetworkSettings.MarshalJSON()
	if err != nil {
		return nil, err
	}

	transportData, err := simplejson.NewJson(transport)
	if err != nil {
		return nil, err
	}

	nodeInfo.NodeType = s.RType
	nodeInfo.NodeID = s.NodeId
	nodeInfo.Address = s.RAddress

	connectPort, err := selectSinglePort(s.RPort)
	if err != nil {
		return nil, fmt.Errorf("failed to parse relay connection port: %w", err)
	}
	nodeInfo.Port = uint16(connectPort)

	listeningPortStr := transportData.Get("listeningPort").MustString()
	selectedPort, err := selectSinglePort(listeningPortStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse relay listening port: %w", err)
	}
	nodeInfo.ListeningPort = uint16(selectedPort)
	nodeInfo.SendThroughIP = transportData.Get("sendThroughIP").MustString()

	if nodeInfo.NodeType == "vless" {
		nodeInfo.Encryption = transportData.Get("encryption").MustString()
		if flow, flowExists := transportData.CheckGet("flow"); flowExists {
			nodeInfo.Flow = flow.MustString()
		}
	}

	if nodeInfo.NodeType == "shadowsocks" {
		nodeInfo.Cipher = transportData.Get("cipher").MustString()
		nodeInfo.ServerKey = s.RServerKey
	}

	if err := parseNetworkSettings(transportData, &nodeInfo.NetworkType, &nodeInfo.HysteriaSettings,
		&nodeInfo.XhttpSettings, &nodeInfo.RawSettings, &nodeInfo.KcpSettings,
		&nodeInfo.GrpcSettings, &nodeInfo.WsSettings, &nodeInfo.HttpSettings); err != nil {
		return nil, err
	}

	security, err := s.RSecuritySettings.MarshalJSON()
	if err != nil {
		return nil, err
	}

	securityData, err := simplejson.NewJson(security)
	if err != nil {
		return nil, err
	}

	if socketSettings, ok := securityData.CheckGet("socketSettings"); ok {
		sock := &SocketSettings{}
		fillSocketSettings(socketSettings, sock)
		nodeInfo.SocketSettings = sock
	}

	if maskSettings, ok := securityData.CheckGet("maskSettings"); ok {
		if err := parseMaskSettingsInto(maskSettings, &nodeInfo.MaskSettings); err != nil {
			return nil, err
		}
	}

	parseRelaySecuritySettings(securityData, nodeInfo)

	return nodeInfo, nil
}

// parseFallbackConfigs parses a "fallbacks" array from the transport settings JSON.
// Expected shape per entry: {"sni":"","alpn":"","path":"","dest":"addr:port","xver":0}
func parseFallbackConfigs(transportData *simplejson.Json) []FallbackConfig {
	fallbacksData, ok := transportData.CheckGet("fallbacks")
	if !ok {
		return nil
	}
	arr, err := fallbacksData.Array()
	if err != nil || len(arr) == 0 {
		return nil
	}
	configs := make([]FallbackConfig, 0, len(arr))
	for i := range arr {
		entry := fallbacksData.GetIndex(i)
		fc := FallbackConfig{}
		if v, err := entry.Get("sni").String(); err == nil {
			fc.SNI = v
		}
		if v, err := entry.Get("alpn").String(); err == nil {
			fc.Alpn = v
		}
		if v, err := entry.Get("path").String(); err == nil {
			fc.Path = v
		}
		if v, err := entry.Get("dest").String(); err == nil {
			fc.Dest = v
		}
		if v, err := entry.Get("xver").Int(); err == nil {
			fc.ProxyProtocolVer = uint64(v)
		}
		if fc.Dest == "" {
			continue // dest is mandatory; skip invalid entries
		}
		configs = append(configs, fc)
	}
	return configs
}

// parseNetworkSettings fills transport-related fields common to both NodeInfo and RelayNodeInfo.
func parseNetworkSettings(
	transportData *simplejson.Json,
	networkType *string,
	hysteria **HysteriaSettings,
	xhttp **XhttpSettings,
	raw **RawSettings,
	kcp **KcpSettings,
	grpc **GrpcSettings,
	ws **WsSettings,
	http **HttpSettings,
) error {
	transport, ok := transportData.CheckGet("transportProtocol")
	if !ok {
		return fmt.Errorf("missing node transportProtocol configuration")
	}

	transportType, typeExist := transport.CheckGet("type")
	if !typeExist {
		return fmt.Errorf("missing node transportProtocol type")
	}

	*networkType = transportType.MustString()
	if *networkType == "" {
		return fmt.Errorf("transportProtocol cannot be empty")
	}

	transportSettings, settingsExist := transport.CheckGet("settings")
	if !settingsExist {
		return fmt.Errorf("missing node transportProtocol settings")
	}

	switch *networkType {
	case "hysteria":
		*hysteria = &HysteriaSettings{
			Version: int32(transportSettings.Get("version").MustInt()),
		}

	case "xhttp":
		x := &XhttpSettings{
			Host: transportSettings.Get("host").MustString(),
			Path: transportSettings.Get("path").MustString(),
			Mode: transportSettings.Get("mode").MustString(),
		}
		x.NoSSEHeader = transportSettings.Get("noSSEHeader").MustBool()
		x.ScMaxEachPostBytes = int32(1000000)
		x.ScMaxBufferedPosts = int64(30)
		x.XPaddingBytes = "100-1000"
		x.ScStreamUpServerSecs = "20-80"
		if v := int32(transportSettings.Get("scMaxEachPostBytes").MustInt()); v > 0 {
			x.ScMaxEachPostBytes = v
		}
		if v := int64(transportSettings.Get("scMaxBufferedPosts").MustInt()); v > 0 {
			x.ScMaxBufferedPosts = v
		}
		if v := transportSettings.Get("scStreamUpServerSecs").MustString(); v != "" {
			x.ScStreamUpServerSecs = v
		}
		if v := transportSettings.Get("xPaddingBytes").MustString(); v != "" {
			x.XPaddingBytes = v
		}
		x.XPaddingObfsMode = transportSettings.Get("xPaddingObfsMode").MustBool()
		if v := transportSettings.Get("xPaddingMethod").MustString(); v != "" {
			x.XPaddingMethod = v
		}
		if v := transportSettings.Get("xPaddingPlacement").MustString(); v != "" {
			x.XPaddingPlacement = v
		}
		if v := transportSettings.Get("xPaddingKey").MustString(); v != "" {
			x.XPaddingKey = v
		}
		if v := transportSettings.Get("xPaddingHeader").MustString(); v != "" {
			x.XPaddingHeader = v
		}
		if v := transportSettings.Get("uplinkHTTPMethod").MustString(); v != "" {
			x.UplinkHTTPMethod = v
		}
		if v := transportSettings.Get("sessionIDPlacement").MustString(); v != "" {
			x.SessionIDPlacement = v
		}
		if v := transportSettings.Get("sessionIDKey").MustString(); v != "" {
			x.SessionIDKey = v
		}
		if v := transportSettings.Get("sessionIDTable").MustString(); v != "" {
			x.SessionIDTable = v
		}
		if v := transportSettings.Get("sessionIDLength").MustString(); v != "" {
			x.SessionIDLength = v
		}
		if v := transportSettings.Get("seqPlacement").MustString(); v != "" {
			x.SeqPlacement = v
		}
		if v := transportSettings.Get("seqKey").MustString(); v != "" {
			x.SeqKey = v
		}
		if v := transportSettings.Get("uplinkDataPlacement").MustString(); v != "" {
			x.UplinkDataPlacement = v
		}
		if v := transportSettings.Get("uplinkDataKey").MustString(); v != "" {
			x.UplinkDataKey = v
		}
		if v := transportSettings.Get("uplinkChunkSize").MustString(); v != "" {
			x.UplinkChunkSize = v
		}
		if xmuxData, ok := transportSettings.CheckGet("xmux"); ok {
			x.Xmux = XmuxConfig{
				MaxConcurrency:   xmuxData.Get("maxConcurrency").MustString(),
				MaxConnections:   xmuxData.Get("maxConnections").MustString(),
				CMaxReuseTimes:   xmuxData.Get("cMaxReuseTimes").MustString(),
				HMaxRequestTimes: xmuxData.Get("hMaxRequestTimes").MustString(),
				HMaxReusableSecs: xmuxData.Get("hMaxReusableSecs").MustString(),
				HKeepAlivePeriod: int64(xmuxData.Get("hKeepAlivePeriod").MustInt()),
			}
		}
		*xhttp = x

	case "raw", "tcp":
		r := &RawSettings{}
		if acceptProxy, ok := transportSettings.CheckGet("acceptProxyProtocol"); ok {
			r.AcceptProxyProtocol = acceptProxy.MustBool()
		}
		if header, ok := transportSettings.CheckGet("header"); ok {
			headerBytes, err := header.MarshalJSON()
			if err != nil {
				return err
			}
			r.Header = headerBytes
		}
		*raw = r

	case "kcp":
		k := &KcpSettings{}
		if mtu, err := transportSettings.Get("mtu").Int(); err == nil {
			k.Mtu = uint32(mtu)
		}
		if tti, err := transportSettings.Get("tti").Int(); err == nil {
			k.Tti = uint32(tti)
		}
		*kcp = k

	case "grpc":
		g := &GrpcSettings{
			ServiceName: transportSettings.Get("servicename").MustString(),
			Authority:   transportSettings.Get("authority").MustString(),
		}
		if v, err := transportSettings.Get("initial_windows_size").Int(); err == nil {
			g.WindowsSize = int32(v)
		}
		if v, err := transportSettings.Get("user_agent").String(); err == nil {
			g.UserAgent = v
		}
		if v, err := transportSettings.Get("idle_timeout").Int(); err == nil {
			g.IdleTimeout = int32(v)
		}
		if v, err := transportSettings.Get("health_check_timeout").Int(); err == nil {
			g.HealthCheckTimeout = int32(v)
		}
		if v, err := transportSettings.Get("permit_without_stream").Bool(); err == nil {
			g.PermitWithoutStream = v
		}
		*grpc = g

	case "ws":
		w := &WsSettings{
			Host:            transportSettings.Get("host").MustString(),
			Path:            transportSettings.Get("path").MustString(),
			HeartbeatPeriod: uint32(transportSettings.Get("heartbeat").MustInt()),
		}
		if v, ok := transportSettings.CheckGet("acceptProxyProtocol"); ok {
			w.AcceptProxyProtocol = v.MustBool()
		}
		*ws = w

	case "httpupgrade":
		h := &HttpSettings{
			Host: transportSettings.Get("host").MustString(),
			Path: transportSettings.Get("path").MustString(),
		}
		if v, ok := transportSettings.CheckGet("acceptProxyProtocol"); ok {
			h.AcceptProxyProtocol = v.MustBool()
		}
		*http = h
	}

	return nil
}

// fillSocketSettings populates a SocketSettings from JSON — shared by node and relay.
func fillSocketSettings(socketSettings *simplejson.Json, s *SocketSettings) {
	s.Enabled = true
	if v, err := socketSettings.Get("acceptProxyProtocol").Bool(); err == nil {
		s.AcceptProxyProtocol = v
	}
	if v, err := socketSettings.Get("domainStrategy").String(); err == nil {
		s.DomainStrategy = v
	}
	if v, err := socketSettings.Get("tcpKeepAliveInterval").Int(); err == nil {
		s.TCPKeepAliveInterval = int32(v)
	}
	if v, err := socketSettings.Get("tcpKeepAliveIdle").Int(); err == nil {
		s.TCPKeepAliveIdle = int32(v)
	}
	if v, err := socketSettings.Get("tcpUserTimeout").Int(); err == nil {
		s.TCPUserTimeout = int32(v)
	}
	if v, err := socketSettings.Get("tcpMaxSeg").Int(); err == nil {
		s.TCPMaxSeg = int32(v)
	}
	if v, err := socketSettings.Get("tcpWindowClamp").Int(); err == nil {
		s.TCPWindowClamp = int32(v)
	}
	if v, err := socketSettings.Get("tcpMptcp").Bool(); err == nil {
		s.TcpMptcp = v
	}
	if v, err := socketSettings.Get("tcpCongestion").String(); err == nil {
		s.TcpCongestion = v
	}
	if v, err := socketSettings.Get("v6only").Bool(); err == nil {
		s.V6only = v
	}
	if v, err := socketSettings.Get("trustedXForwardedFor").StringArray(); err == nil {
		s.TrustedXForwardedFor = v
	}
	if tfoData, ok := socketSettings.CheckGet("tcpFastOpen"); ok {
		if raw, err := tfoData.MarshalJSON(); err == nil {
			var tfo interface{}
			if json.Unmarshal(raw, &tfo) == nil {
				s.TFO = tfo
			}
		}
	}
}

func parseSecuritySettings(
	securityData *simplejson.Json,
	securityType *string,
	tlsSettings **TlsSettings,
	realitySettings **RealitySettings,
	finalRules *[]FinalRuleSettings,
) error {
	*securityType = "none"

	if tlsData, ok := securityData.CheckGet("tlsSettings"); ok {
		*securityType = "tls"
		tls := &TlsSettings{CertMode: "none"}

		if certMode, err := tlsData.Get("certMode").String(); err == nil {
			tls.CertMode = certMode
		}
		if certDomain, ok := tlsData.CheckGet("certDomainName"); ok {
			if name, err := certDomain.String(); err == nil {
				tls.CertDomainName = name
			} else if tls.CertMode != "none" {
				return fmt.Errorf("certificate domain name is required")
			}
		} else {
			return fmt.Errorf("certDomainName key missing from tlsSettings")
		}
		if certEmail, ok := tlsData.CheckGet("certEmail"); ok {
			if email, err := certEmail.String(); err == nil {
				tls.CertEmail = email
			}
		}
		if serverName, ok := tlsData.CheckGet("serverName"); ok {
			if name, err := serverName.String(); err == nil {
				tls.ServerName = name
			}
		}
		if fp, err := tlsData.Get("fingerprint").String(); err == nil {
			tls.FingerPrint = fp
		}
		if curves, err := tlsData.Get("curvePreferences").StringArray(); err == nil {
			tls.CurvePreferences = curves
		}
		if reject, err := tlsData.Get("rejectUnknownSni").Bool(); err == nil {
			tls.RejectUnknownSni = reject
		}
		if alpn, err := tlsData.Get("alpn").StringArray(); err == nil {
			tls.Alpn = alpn
		}
		if echKeys, err := tlsData.Get("echServerKeys").String(); err == nil {
			tls.ECHServerKeys = echKeys
		}
		if v, err := tlsData.Get("dnsProvider").String(); err == nil {
			tls.DnsProvider = v
		}
		if v, err := tlsData.Get("certFile").String(); err == nil {
			tls.CertFile = v
		}
		if v, err := tlsData.Get("keyFile").String(); err == nil {
			tls.KeyFile = v
		}
		if v, err := tlsData.Get("cipherSuites").String(); err == nil {
			tls.CipherSuites = v
		}
		if v, err := tlsData.Get("minVersion").String(); err == nil {
			tls.MinVersion = v
		}
		if v, err := tlsData.Get("maxVersion").String(); err == nil {
			tls.MaxVersion = v
		}
		*tlsSettings = tls
	}

	if realityData, ok := securityData.CheckGet("realitySettings"); ok {
		*securityType = "reality"
		reality := &RealitySettings{}

		if dest, err := realityData.Get("target").String(); err == nil {
			destBytes, err := json.Marshal(dest)
			if err != nil {
				return err
			}
			reality.Dest = json.RawMessage(destBytes)
		}
		if show, err := realityData.Get("show").Bool(); err == nil {
			reality.Show = show
		}
		if v, err := realityData.Get("minClientVer").String(); err == nil {
			reality.MinClientVer = v
		}
		if v, err := realityData.Get("maxClientVer").String(); err == nil {
			reality.MaxClientVer = v
		}
		if v, err := realityData.Get("maxTimeDiff").Int(); err == nil {
			reality.MaxTimeDiff = uint64(v)
		}
		if v, err := realityData.Get("proxyprotocol").Int(); err == nil {
			reality.Xver = uint64(v)
		}
		if v, err := realityData.Get("serverNames").StringArray(); err == nil {
			reality.ServerNames = v
		}
		if v, err := realityData.Get("shortids").StringArray(); err == nil {
			reality.ShortIds = v
		}
		if v, err := realityData.Get("mldsa65Seed").String(); err == nil {
			reality.Mldsa65Seed = v
		}
		if v, err := realityData.Get("privateKey").String(); err == nil {
			reality.PrivateKey = v
		}
		*realitySettings = reality
	}

	*finalRules = parseFinalRules(securityData)
	return nil
}

func parseRelaySecuritySettings(securityData *simplejson.Json, nodeInfo *RelayNodeInfo) {
	nodeInfo.SecurityType = "none"

	if tlsData, ok := securityData.CheckGet("tlsSettings"); ok {
		nodeInfo.SecurityType = "tls"
		tls := &TlsSettings{}
		if fp, err := tlsData.Get("fingerprint").String(); err == nil {
			tls.FingerPrint = fp
		}
		if serverName, ok := tlsData.CheckGet("serverName"); ok {
			if name, err := serverName.String(); err == nil {
				tls.ServerName = name
			}
		}
		if v, ok := tlsData.CheckGet("verifyPeerCertByName"); ok {
			if name, err := v.String(); err == nil {
				tls.VerifyPeerCertByName = name
			}
		}
		if v, err := tlsData.Get("echConfigList").String(); err == nil {
			tls.ECHConfigList = v
		}
		if v, err := tlsData.Get("pinnedPeerCertSha256").String(); err == nil {
			tls.PinnedPeerCertSha256 = v
		}
		nodeInfo.TlsSettings = tls
	}

	if realityData, ok := securityData.CheckGet("realitySettings"); ok {
		nodeInfo.SecurityType = "reality"
		reality := &RealitySettings{}
		if show, err := realityData.Get("show").Bool(); err == nil {
			reality.Show = show
		}
		if v, err := realityData.Get("password").String(); err == nil {
			reality.PublicKey = v
		}
		if v, err := realityData.Get("serverName").String(); err == nil {
			reality.ServerName = v
		}
		if v, err := realityData.Get("shortid").String(); err == nil {
			reality.ShortId = v
		}
		if v, err := realityData.Get("spiderX").String(); err == nil {
			reality.SpiderX = v
		}
		if v, err := realityData.Get("fingerprint").String(); err == nil {
			reality.Fingerprint = v
		}
		if v, err := realityData.Get("mldsa65Verify").String(); err == nil {
			reality.Mldsa65Verify = v
		}
		nodeInfo.RealitySettings = reality
	}

	nodeInfo.FinalRules = parseFinalRules(securityData)
}

func parseFinalRules(securityData *simplejson.Json) []FinalRuleSettings {
	var rules []FinalRuleSettings
	if finalRulesData, ok := securityData.CheckGet("finalRules"); ok {
		arr, err := finalRulesData.Array()
		if err == nil {
			for i := range arr {
				rule := finalRulesData.GetIndex(i)
				fr := FinalRuleSettings{}
				if v, err := rule.Get("action").String(); err == nil {
					fr.Action = v
				}
				if v, err := rule.Get("network").String(); err == nil {
					fr.Network = v
				}
				if v, err := rule.Get("port").String(); err == nil {
					fr.Port = v
				}
				if v, err := rule.Get("ip").StringArray(); err == nil {
					fr.IP = v
				}
				if v, err := rule.Get("blockDelay").String(); err == nil {
					fr.BlockDelay = v
				}
				rules = append(rules, fr)
			}
		}
	}
	return rules
}

func parseBlockingRules(ruleData *simplejson.Json) *BlockingRules {
	rules := &BlockingRules{}
	if ipData, ok := ruleData.CheckGet("ip"); ok {
		if arr, err := ipData.StringArray(); err == nil {
			rules.IP = arr
		}
	}
	if domainData, ok := ruleData.CheckGet("domain"); ok {
		if arr, err := domainData.StringArray(); err == nil {
			rules.Domain = arr
		}
	}
	if portData, ok := ruleData.CheckGet("port"); ok {
		if s, err := portData.String(); err == nil {
			rules.Port = s
		}
	}
	if protocolData, ok := ruleData.CheckGet("protocol"); ok {
		if arr, err := protocolData.StringArray(); err == nil {
			rules.Protocol = arr
		}
	}
	return rules
}

func parseMaskSettingsInto(maskSettings *simplejson.Json, ms **MaskSettings) error {
	var tcpMasks []MaskEntry
	var udpMasks []MaskEntry
	var quicParams *QuicParamsSettings

	*ms = &MaskSettings{}

	if maskTCP, ok := maskSettings.CheckGet("tcp"); ok {
		arr, err := maskTCP.Array()
		if err != nil {
			return err
		}
		for i := range arr {
			entry, err := parseSingleMask(maskTCP.GetIndex(i))
			if err != nil {
				return fmt.Errorf("tcp mask[%d]: %w", i, err)
			}
			tcpMasks = append(tcpMasks, *entry)
		}
	}

	if maskUDP, ok := maskSettings.CheckGet("udp"); ok {
		arr, err := maskUDP.Array()
		if err != nil {
			return err
		}
		for i := range arr {
			entry, err := parseSingleMask(maskUDP.GetIndex(i))
			if err != nil {
				return fmt.Errorf("udp mask[%d]: %w", i, err)
			}
			udpMasks = append(udpMasks, *entry)
		}
	}

	if qp, ok := maskSettings.CheckGet("quicParams"); ok {
		parsed, err := parseQuicParams(qp)
		if err != nil {
			return fmt.Errorf("quicParams: %w", err)
		}
		quicParams = parsed
		(*ms).EnabledQuic = true
	}

	if len(tcpMasks) == 0 && len(udpMasks) == 0 && quicParams == nil {
		return nil
	}

	(*ms).Enabled = true
	(*ms).TCP = tcpMasks
	(*ms).UDP = udpMasks
	(*ms).QuicParams = quicParams

	return nil
}

func parseQuicParams(qp *simplejson.Json) (*QuicParamsSettings, error) {
	q := &QuicParamsSettings{}

	if v, err := qp.Get("congestion").String(); err == nil {
		q.Congestion = v
	}
	if v, err := qp.Get("debug").Bool(); err == nil {
		q.Debug = v
	}
	if v, err := qp.Get("bbrProfile").String(); err == nil {
		q.BbrProfile = v
	}
	if v, err := qp.Get("brutalUp").String(); err == nil {
		q.BrutalUp = v
	}
	if v, err := qp.Get("brutalDown").String(); err == nil {
		q.BrutalDown = v
	}
	if v, err := qp.Get("initStreamReceiveWindow").Uint64(); err == nil {
		q.InitStreamReceiveWindow = v
	}
	if v, err := qp.Get("maxStreamReceiveWindow").Uint64(); err == nil {
		q.MaxStreamReceiveWindow = v
	}
	if v, err := qp.Get("initConnectionReceiveWindow").Uint64(); err == nil {
		q.InitConnectionReceiveWindow = v
	}
	if v, err := qp.Get("maxConnectionReceiveWindow").Uint64(); err == nil {
		q.MaxConnectionReceiveWindow = v
	}
	if v, err := qp.Get("maxIdleTimeout").Int64(); err == nil {
		q.MaxIdleTimeout = v
	}
	if v, err := qp.Get("keepAlivePeriod").Int64(); err == nil {
		q.KeepAlivePeriod = v
	}
	if v, err := qp.Get("disablePathMTUDiscovery").Bool(); err == nil {
		q.DisablePathMTUDiscovery = v
	}
	if v, err := qp.Get("maxIncomingStreams").Int64(); err == nil {
		q.MaxIncomingStreams = v
	}
	if v, err := qp.Get("brutalDisableLossCompensation").Bool(); err == nil {
		q.BrutalDisableLossCompensation = v
	}
	if v, err := qp.Get("disableGSO").Bool(); err == nil {
		q.DisableGSO = v
	}
	if v, err := qp.Get("disableChromeParrot").Bool(); err == nil {
		q.DisableChromeParrot = v
	}
	if v, err := qp.Get("disableStatelessReset").Bool(); err == nil {
		q.DisableStatelessReset = v
	}

	return q, nil
}

func parseSingleMask(mask *simplejson.Json) (*MaskEntry, error) {
	entry := &MaskEntry{}
	if maskType, err := mask.Get("type").String(); err == nil {
		entry.Type = maskType
	}
	settings, ok := mask.CheckGet("settings")
	if !ok {
		return entry, nil
	}
	raw, err := settings.MarshalJSON()
	if err != nil {
		return nil, err
	}
	rm := json.RawMessage(raw)
	entry.Settings = &rm
	return entry, nil
}

func selectSinglePort(portString string) (uint32, error) {
	if portString == "" {
		return 0, fmt.Errorf("port string is empty")
	}

	var allPorts []uint32

	if strings.Contains(portString, ",") {
		for _, p := range strings.Split(portString, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			ports, err := expandPortRange(p)
			if err != nil {
				return 0, err
			}
			allPorts = append(allPorts, ports...)
		}
	} else if strings.Contains(portString, "-") {
		ports, err := expandPortRange(portString)
		if err != nil {
			return 0, err
		}
		allPorts = append(allPorts, ports...)
	} else {
		port, err := strconv.ParseUint(portString, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid port number: %s", portString)
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("port out of range: %d", port)
		}
		return uint32(port), nil
	}

	if len(allPorts) == 0 {
		return 0, fmt.Errorf("no valid ports found in: %s", portString)
	}
	return allPorts[rand.Intn(len(allPorts))], nil
}

func expandPortRange(p string) ([]uint32, error) {
	if strings.Contains(p, "-") {
		parts := strings.SplitN(p, "-", 2)
		from, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid port in range: %s", parts[0])
		}
		to, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid port in range: %s", parts[1])
		}
		if from < 1 || from > 65535 || to < 1 || to > 65535 {
			return nil, fmt.Errorf("port out of range: %d-%d", from, to)
		}
		var ports []uint32
		for i := from; i <= to; i++ {
			ports = append(ports, uint32(i))
		}
		return ports, nil
	}
	port, err := strconv.ParseUint(p, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid port number: %s", p)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("port out of range: %d", port)
	}
	return []uint32{uint32(port)}, nil
}

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
