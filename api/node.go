package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"errors"
	"math/rand"
	"strconv"
    
	"github.com/bitly/go-simplejson"
	"github.com/xtls/xray-core/infra/conf"
)

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

	nodeInfo.NetworkType = ""
	nodeInfo.NodeType = strings.ToLower(s.Type)
	nodeInfo.NodeID = c.NodeID
	nodeInfo.RelayNodeID = int(s.RelayNodeId)
	nodeInfo.RelayType = int(s.RelayType)
	nodeInfo.SpeedLimit = uint64(s.Speedlimit * 1000000 / 8)
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

	// Parse network transport settings
	if err := c.parseNetworkSettings(transportData, nodeInfo); err != nil {
		return nil, err
	}

	// Parse socket settings
	if socketSettings, ok := transportData.CheckGet("socketSettings"); ok {
		if err := c.parseSocketSettings(socketSettings, nodeInfo); err != nil {
			return nil, err
		}
	}

	// Parse security settings
	security, err := s.SecuritySettings.MarshalJSON()
	if err != nil {
		return nil, err
	}

	securityData, err := simplejson.NewJson(security)
	if err != nil {
		return nil, err
	}

	if err := c.parseSecuritySettings(securityData, nodeInfo); err != nil {
		return nil, err
	}
	
	// Parse mask settings
	if maskSettings, ok := securityData.CheckGet("maskSettings"); ok {
		if err := c.parseMaskSettings(maskSettings, nodeInfo); err != nil {
			return nil, err
		}
	}

	// Parse blocking rules
	rule, err := s.Rules.MarshalJSON()
	if err != nil {
		return nil, err
	}

	ruleData, err := simplejson.NewJson(rule)
	if err != nil {
		return nil, err
	}

	c.parseBlockingRules(ruleData, nodeInfo)

	return nodeInfo, nil
}

func (c *Client) parseNetworkSettings(transportData *simplejson.Json, nodeInfo *NodeInfo) error {
	
	transport, ok := transportData.CheckGet("transportProtocol")
	if !ok {
		return fmt.Errorf("Missing node transportProtocol configuration.")
	}
	
	transportType, typeExist := transport.CheckGet("type")
	if !typeExist {
		return fmt.Errorf("Missing node transportProtocol type.")
	}
	
	nodeInfo.NetworkType = transportType.MustString()
	if nodeInfo.NetworkType == "" {
		return fmt.Errorf("transportProtocol cannot be empty.")
	}
	
	transportSettings, settingsExist := transport.CheckGet("settings")
	if !settingsExist {
		return fmt.Errorf("Missing node transportProtocol settings.")
	}
	
	//hysteria
	if nodeInfo.NetworkType == "hysteria" {
		nodeInfo.HysteriaSettings = &HysteriaSettings{
			Version: int32(transportSettings.Get("version").MustInt()),
		}
		
		if nodeInfo.NodeType == "hysteria" && nodeInfo.NetworkType != "hysteria" {
			return fmt.Errorf("Hysteria only accept hysteria transportProtocol")
		}
	}
	
	//xhttp	
	if nodeInfo.NetworkType == "xhttp" {
		nodeInfo.XhttpSettings = &XhttpSettings{
			Host: transportSettings.Get("host").MustString(),
			Path: transportSettings.Get("path").MustString(),
			Mode: transportSettings.Get("mode").MustString(),
		}

		// Set defaults first
		nodeInfo.XhttpSettings.NoSSEHeader = bool(false)
		nodeInfo.XhttpSettings.ScMaxEachPostBytes = int32(1000000)
		nodeInfo.XhttpSettings.ScMaxBufferedPosts = int64(30)
		nodeInfo.XhttpSettings.XPaddingBytes = "100-100"
		nodeInfo.XhttpSettings.ScStreamUpServerSecs = "20-80"

		nodeInfo.XhttpSettings.NoSSEHeader = transportSettings.Get("noSSEHeader").MustBool()
		nodeInfo.XhttpSettings.ScMaxEachPostBytes = int32(transportSettings.Get("scMaxEachPostBytes").MustInt())
		nodeInfo.XhttpSettings.ScMaxBufferedPosts = int64(transportSettings.Get("scMaxBufferedPosts").MustInt())
		nodeInfo.XhttpSettings.ScStreamUpServerSecs = transportSettings.Get("scStreamUpServerSecs").MustString()
		nodeInfo.XhttpSettings.XPaddingBytes = transportSettings.Get("xPaddingBytes").MustString()
	}
	
	//raw
	if nodeInfo.NetworkType == "raw" || nodeInfo.NetworkType == "tcp" {
		nodeInfo.RawSettings = &RawSettings{}
		if acceptProxy, protocolExists := transportSettings.CheckGet("acceptProxyProtocol"); protocolExists {
			nodeInfo.RawSettings.AcceptProxyProtocol = acceptProxy.MustBool()
		}
		if header, headerExist := transportSettings.CheckGet("header"); headerExist {
			headerBytes, err := header.MarshalJSON()
			if err != nil {
				return err
			}
			nodeInfo.RawSettings.Header = headerBytes
		}
	}
	
	//kcp
	if nodeInfo.NetworkType == "kcp" {
		nodeInfo.KcpSettings = &KcpSettings{}
		if MtuData, err := transportSettings.Get("mtu").Int(); err == nil {
			nodeInfo.KcpSettings.Mtu = uint32(MtuData)
		}
	}
	
    //grpc
	if nodeInfo.NetworkType == "grpc" {
		nodeInfo.GrpcSettings = &GrpcSettings{
			ServiceName: transportSettings.Get("servicename").MustString(),
			Authority: transportSettings.Get("authority").MustString(),
		}
		if sizeData, err := transportSettings.Get("initial_windows_size").Int(); err == nil {
			nodeInfo.GrpcSettings.WindowsSize = int32(sizeData)
		}
		if agentData, err := transportSettings.Get("user_agent").String(); err == nil {
			nodeInfo.GrpcSettings.UserAgent = agentData
		}
		if timeoutData, err := transportSettings.Get("idle_timeout").Int(); err == nil {
			nodeInfo.GrpcSettings.IdleTimeout = int32(timeoutData)
		}
		if checkData, err := transportSettings.Get("health_check_timeout").Int(); err == nil {
			nodeInfo.GrpcSettings.HealthCheckTimeout = int32(checkData)
		}
		if permitData, err := transportSettings.Get("permit_without_stream").Bool(); err == nil {
			nodeInfo.GrpcSettings.PermitWithoutStream = permitData
		}
	}

	//ws
	if nodeInfo.NetworkType == "ws" {
		nodeInfo.WsSettings = &WsSettings{
			Host: transportSettings.Get("host").MustString(),
			Path: transportSettings.Get("path").MustString(),
			HeartbeatPeriod: uint32(transportSettings.Get("heartbeat").MustInt()),
		}
		if acceptProxy, protocolExists := transportSettings.CheckGet("acceptProxyProtocol"); protocolExists {
			nodeInfo.WsSettings.AcceptProxyProtocol = acceptProxy.MustBool()
		}
	}

	//httpupgrade
	if nodeInfo.NetworkType == "httpupgrade" {	
		nodeInfo.HttpSettings = &HttpSettings{
			Host: transportSettings.Get("host").MustString(),
			Path: transportSettings.Get("path").MustString(),
		}
		if acceptProxy, protocolExists := transportSettings.CheckGet("acceptProxyProtocol"); protocolExists {
			nodeInfo.HttpSettings.AcceptProxyProtocol = acceptProxy.MustBool()
		}
	}

	return nil
}

func (c *Client) parseMaskSettings(maskSettings *simplejson.Json, nodeInfo *NodeInfo) error {
	return parseMaskSettingsInto(maskSettings, &nodeInfo.MaskSettings)
}

func (c *Client) parseSocketSettings(socketSettings *simplejson.Json, nodeInfo *NodeInfo) error {
	nodeInfo.SocketSettings = &SocketSettings{}
	nodeInfo.SocketSettings.Enabled = true
	
	if acceptProxy, protocolExists := socketSettings.CheckGet("acceptProxyProtocol"); protocolExists {
		nodeInfo.SocketSettings.AcceptProxyProtocol = acceptProxy.MustBool()
	}

	if val, err := socketSettings.Get("tCPKeepAliveInterval").Int(); err == nil {
		nodeInfo.SocketSettings.TCPKeepAliveInterval = int32(val)
	}
	if val, err := socketSettings.Get("tCPKeepAliveIdle").Int(); err == nil {
		nodeInfo.SocketSettings.TCPKeepAliveIdle = int32(val)
	}
	if val, err := socketSettings.Get("tCPUserTimeout").Int(); err == nil {
		nodeInfo.SocketSettings.TCPUserTimeout = int32(val)
	}
	if val, err := socketSettings.Get("tCPMaxSeg").Int(); err == nil {
		nodeInfo.SocketSettings.TCPMaxSeg = int32(val)
	}
	if val, err := socketSettings.Get("tCPWindowClamp").Int(); err == nil {
		nodeInfo.SocketSettings.TCPWindowClamp = int32(val)
	}
	if val, err := socketSettings.Get("tcpMptcp").Bool(); err == nil {
		nodeInfo.SocketSettings.TcpMptcp = val
	}
	if val, err := socketSettings.Get("domainStrategy").String(); err == nil {
		nodeInfo.SocketSettings.DomainStrategy = val
	}
	
	if val, err := socketSettings.Get("tcpCongestion").String(); err == nil {
		nodeInfo.SocketSettings.TcpCongestion = val
	}

	return nil
}

func (c *Client) parseSecuritySettings(securityData *simplejson.Json, nodeInfo *NodeInfo) error {
	nodeInfo.SecurityType = "none"

	if tlsSettings, ok := securityData.CheckGet("tlsSettings"); ok {
		nodeInfo.SecurityType = "tls"
		nodeInfo.TlsSettings = &TlsSettings{
			CertMode: "none",
		}

		if certMode, err := tlsSettings.Get("certMode").String(); err == nil {
			nodeInfo.TlsSettings.CertMode = certMode
		}

		if certDomain, ok := tlsSettings.CheckGet("certDomainName"); ok {
			if certDomainName, err := certDomain.String(); err == nil {
				nodeInfo.TlsSettings.CertDomainName = certDomainName
			} else if nodeInfo.TlsSettings.CertMode != "none" {
				return fmt.Errorf("certificate domain name is required")
			}
		} else {
			return fmt.Errorf("certDomainName key missing from tlsSettings")
		}

		if serverName, ok := tlsSettings.CheckGet("serverName"); ok {
			if serverNameData, err := serverName.String(); err == nil {
				nodeInfo.TlsSettings.ServerName = serverNameData
			}
		}
		if fingerprint, err := tlsSettings.Get("fingerprint").String(); err == nil {
			nodeInfo.TlsSettings.FingerPrint = fingerprint
		}
		if curvePreferences, err := tlsSettings.Get("curvePreferences").StringArray(); err == nil {
			nodeInfo.TlsSettings.CurvePreferences = curvePreferences
		}
		if rejectUnknownSni, err := tlsSettings.Get("rejectUnknownSni").Bool(); err == nil {
			nodeInfo.TlsSettings.RejectUnknownSni = rejectUnknownSni
		}
		if alpnArray, err := tlsSettings.Get("alpn").StringArray(); err == nil {
			nodeInfo.TlsSettings.Alpn = alpnArray
		}
		if echServerKeys, err := tlsSettings.Get("echServerKeys").String(); err == nil {
			nodeInfo.TlsSettings.ECHServerKeys = echServerKeys
		}
	}

	if realitySettings, ok := securityData.CheckGet("realitySettings"); ok {
		nodeInfo.SecurityType = "reality"
		nodeInfo.RealitySettings = &RealitySettings{}

		if dest, err := realitySettings.Get("target").String(); err == nil {
			destBytes, err := json.Marshal(dest)
			if err != nil {
				return err
			}
			nodeInfo.RealitySettings.Dest = json.RawMessage(destBytes)
		}
		if show, err := realitySettings.Get("show").Bool(); err == nil {
			nodeInfo.RealitySettings.Show = show
		}
		if minClientVer, err := realitySettings.Get("minClientVer").String(); err == nil {
			nodeInfo.RealitySettings.MinClientVer = minClientVer
		}
		if maxClientVer, err := realitySettings.Get("maxClientVer").String(); err == nil {
			nodeInfo.RealitySettings.MaxClientVer = maxClientVer
		}
		if maxTimeDiff, err := realitySettings.Get("maxTimeDiff").Int(); err == nil {
			nodeInfo.RealitySettings.MaxTimeDiff = uint64(maxTimeDiff)
		}
		if xver, err := realitySettings.Get("proxyprotocol").Int(); err == nil {
			nodeInfo.RealitySettings.Xver = uint64(xver)
		}
		if serverNamesArray, err := realitySettings.Get("serverNames").StringArray(); err == nil {
			nodeInfo.RealitySettings.ServerNames = serverNamesArray
		}
		if shortIdsArray, err := realitySettings.Get("shortids").StringArray(); err == nil {
			nodeInfo.RealitySettings.ShortIds = shortIdsArray
		}
		if mldsa65Seed, err := realitySettings.Get("mldsa65Seed").String(); err == nil {
			nodeInfo.RealitySettings.Mldsa65Seed = mldsa65Seed
		}
		if privateKey, err := realitySettings.Get("privateKey").String(); err == nil {
			nodeInfo.RealitySettings.PrivateKey = privateKey
		}
	}

	return nil
}

func (c *Client) parseBlockingRules(ruleData *simplejson.Json, nodeInfo *NodeInfo) {
	nodeInfo.BlockingRules = &BlockingRules{}

	if ipData, ipKeyExists := ruleData.CheckGet("ip"); ipKeyExists {
		if ipArray, err := ipData.StringArray(); err == nil {
			nodeInfo.BlockingRules.IP = ipArray
		}
	}
	if domainData, domainKeyExists := ruleData.CheckGet("domain"); domainKeyExists {
		if domainArray, err := domainData.StringArray(); err == nil {
			nodeInfo.BlockingRules.Domain = domainArray
		}
	}
	if portData, portKeyExists := ruleData.CheckGet("port"); portKeyExists {
		if portStr, err := portData.String(); err == nil {
			nodeInfo.BlockingRules.Port = portStr
		}
	}
	if protocolData, protocolKeyExists := ruleData.CheckGet("protocol"); protocolKeyExists {
		if protocolArray, err := protocolData.StringArray(); err == nil {
			nodeInfo.BlockingRules.Protocol = protocolArray
		}
	}
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

	nodeInfo.NetworkType = ""
	nodeInfo.NodeType = s.RType
	nodeInfo.NodeID = s.NodeId
	nodeInfo.Address = s.RAddress

	// Parse listening port using utility function to handle all formats
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
		nodeInfo.ServerKey = s.ServerKey
	}

	// Parse relay network settings
	if err := c.parseRelayNetworkSettings(transportData, nodeInfo); err != nil {
		return nil, err
	}

	// Parse relay security settings
	security, err := s.RSecuritySettings.MarshalJSON()
	if err != nil {
		return nil, err
	}

	securityData, err := simplejson.NewJson(security)
	if err != nil {
		return nil, err
	}
	
	// Parse mask settings
	if maskSettings, ok := securityData.CheckGet("maskSettings"); ok {
		if err := c.parseRelayMaskSettings(maskSettings, nodeInfo); err != nil {
			return nil, err
		}
	}

	c.parseRelaySecuritySettings(securityData, nodeInfo)

	return nodeInfo, nil
}

func selectSinglePort(portString string) (uint32, error) {
	if portString == "" {
		return 0, fmt.Errorf("port string is empty")
	}

	var allPorts []uint32

	// Check if it contains comma (multiple ports/ranges)
	if strings.Contains(portString, ",") {
		// Split by comma
		ports := strings.Split(portString, ",")
		
		for _, p := range ports {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			
			// Check if it's a range (contains "-")
			if strings.Contains(p, "-") {
				rangeParts := strings.SplitN(p, "-", 2)
				if len(rangeParts) != 2 {
					return 0, fmt.Errorf("invalid port range format: %s", p)
				}
				
				fromPort, err := strconv.ParseUint(strings.TrimSpace(rangeParts[0]), 10, 32)
				if err != nil {
					return 0, fmt.Errorf("invalid port number in range: %s", rangeParts[0])
				}
				
				toPort, err := strconv.ParseUint(strings.TrimSpace(rangeParts[1]), 10, 32)
				if err != nil {
					return 0, fmt.Errorf("invalid port number in range: %s", rangeParts[1])
				}
				
				// Validate port range
				if fromPort < 1 || fromPort > 65535 || toPort < 1 || toPort > 65535 {
					return 0, fmt.Errorf("port number out of valid range (1-65535): %d-%d", fromPort, toPort)
				}
				
				// Add all ports in range
				for i := fromPort; i <= toPort; i++ {
					allPorts = append(allPorts, uint32(i))
				}
			} else {
				// Single port
				port, err := strconv.ParseUint(p, 10, 32)
				if err != nil {
					return 0, fmt.Errorf("invalid port number: %s", p)
				}
				
				// Validate port number
				if port < 1 || port > 65535 {
					return 0, fmt.Errorf("port number out of valid range (1-65535): %d", port)
				}
				
				allPorts = append(allPorts, uint32(port))
			}
		}
	} else if strings.Contains(portString, "-") {
		// Single range (e.g., "1000-2000")
		rangeParts := strings.SplitN(portString, "-", 2)
		if len(rangeParts) != 2 {
			return 0, fmt.Errorf("invalid port range format: %s", portString)
		}
		
		fromPort, err := strconv.ParseUint(strings.TrimSpace(rangeParts[0]), 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid port number in range: %s", rangeParts[0])
		}
		
		toPort, err := strconv.ParseUint(strings.TrimSpace(rangeParts[1]), 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid port number in range: %s", rangeParts[1])
		}
		
		// Validate port range
		if fromPort < 1 || fromPort > 65535 || toPort < 1 || toPort > 65535 {
			return 0, fmt.Errorf("port number out of valid range (1-65535): %d-%d", fromPort, toPort)
		}
		
		// Add all ports in range
		for i := fromPort; i <= toPort; i++ {
			allPorts = append(allPorts, uint32(i))
		}
	} else {
		// Single port (e.g., "443")
		port, err := strconv.ParseUint(portString, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid port number: %s", portString)
		}
		
		// Validate port number
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("port number out of valid range (1-65535): %d", port)
		}
		
		return uint32(port), nil
	}

	if len(allPorts) == 0 {
		return 0, fmt.Errorf("no valid ports found in: %s", portString)
	}

	// Randomly select one port from the available ports
	selectedPort := allPorts[rand.Intn(len(allPorts))]
	
	return selectedPort, nil
}

func (c *Client) parseRelayMaskSettings(maskSettings *simplejson.Json, nodeInfo *RelayNodeInfo) error {
	return parseMaskSettingsInto(maskSettings, &nodeInfo.MaskSettings)
}

func (c *Client) parseRelayNetworkSettings(transportData *simplejson.Json, nodeInfo *RelayNodeInfo) error {
	transport, ok := transportData.CheckGet("transportProtocol")
	if !ok {
		return fmt.Errorf("Missing node transportProtocol configuration.")
	}
	
	transportType, typeExist := transport.CheckGet("type")
	if !typeExist {
		return fmt.Errorf("Missing node transportProtocol type.")
	}
	
	nodeInfo.NetworkType = transportType.MustString()
	
	transportSettings, settingsExist := transport.CheckGet("settings")
	if !settingsExist {
		return fmt.Errorf("Missing node transportProtocol settings.")
	}
	
	//hysteria
	if nodeInfo.NetworkType == "hysteria" {
		nodeInfo.HysteriaSettings = &HysteriaSettings{
			Version: int32(transportSettings.Get("version").MustInt()),
		}
		
		if nodeInfo.NodeType == "hysteria" && nodeInfo.NetworkType != "hysteria" {
			return fmt.Errorf("Hysteria only accept hysteria transportProtocol")
		}
	}
	
	//xhttp	
	if nodeInfo.NetworkType == "xhttp" {
		nodeInfo.XhttpSettings = &XhttpSettings{
			Host: transportSettings.Get("host").MustString(),
			Path: transportSettings.Get("path").MustString(),
			Mode: transportSettings.Get("mode").MustString(),
		}
	}
	
	//raw
	if nodeInfo.NetworkType == "raw" || nodeInfo.NetworkType == "tcp" {
		nodeInfo.RawSettings = &RawSettings{}
		if acceptProxy, protocolExists := transportSettings.CheckGet("acceptProxyProtocol"); protocolExists {
			nodeInfo.RawSettings.AcceptProxyProtocol = acceptProxy.MustBool()
		}
		if header, headerExist := transportSettings.CheckGet("header"); headerExist {
			headerBytes, err := header.MarshalJSON()
			if err != nil {
				return err
			}
			nodeInfo.RawSettings.Header = headerBytes
		}
	}
	
	//kcp
	if nodeInfo.NetworkType == "kcp" {
		nodeInfo.KcpSettings = &KcpSettings{}
		if MtuData, err := transportSettings.Get("mtu").Int(); err == nil {
			nodeInfo.KcpSettings.Mtu = uint32(MtuData)
		}
	}
	
    //grpc
	if nodeInfo.NetworkType == "grpc" {
		nodeInfo.GrpcSettings = &GrpcSettings{
			ServiceName: transportSettings.Get("servicename").MustString(),
			Authority: transportSettings.Get("authority").MustString(),
		}
		if sizeData, err := transportSettings.Get("initial_windows_size").Int(); err == nil {
			nodeInfo.GrpcSettings.WindowsSize = int32(sizeData)
		}
		if agentData, err := transportSettings.Get("user_agent").String(); err == nil {
			nodeInfo.GrpcSettings.UserAgent = agentData
		}
		if timeoutData, err := transportSettings.Get("idle_timeout").Int(); err == nil {
			nodeInfo.GrpcSettings.IdleTimeout = int32(timeoutData)
		}
		if checkData, err := transportSettings.Get("health_check_timeout").Int(); err == nil {
			nodeInfo.GrpcSettings.HealthCheckTimeout = int32(checkData)
		}
		if permitData, err := transportSettings.Get("permit_without_stream").Bool(); err == nil {
			nodeInfo.GrpcSettings.PermitWithoutStream = permitData
		}
	}

	//ws
	if nodeInfo.NetworkType == "ws" {
		nodeInfo.WsSettings = &WsSettings{
			Host: transportSettings.Get("host").MustString(),
			Path: transportSettings.Get("path").MustString(),
			HeartbeatPeriod: uint32(transportSettings.Get("heartbeat").MustInt()),
		}
		if acceptProxy, protocolExists := transportSettings.CheckGet("acceptProxyProtocol"); protocolExists {
			nodeInfo.WsSettings.AcceptProxyProtocol = acceptProxy.MustBool()
		}
	}

	//httpupgrade
	if nodeInfo.NetworkType == "httpupgrade" {	
		nodeInfo.HttpSettings = &HttpSettings{
			Host: transportSettings.Get("host").MustString(),
			Path: transportSettings.Get("path").MustString(),
		}
		if acceptProxy, protocolExists := transportSettings.CheckGet("acceptProxyProtocol"); protocolExists {
			nodeInfo.HttpSettings.AcceptProxyProtocol = acceptProxy.MustBool()
		}
	}

	return nil
}

func (c *Client) parseRelaySecuritySettings(securityData *simplejson.Json, nodeInfo *RelayNodeInfo) {
	nodeInfo.SecurityType = "none"

	if tlsSettings, ok := securityData.CheckGet("tlsSettings"); ok {
		nodeInfo.SecurityType = "tls"
		nodeInfo.TlsSettings = &TlsSettings{}
		
		if Insecure, err := tlsSettings.Get("allowInsecure").Bool(); err == nil {
			nodeInfo.TlsSettings.AllowInsecure = Insecure
		}

		if fingerprint, err := tlsSettings.Get("fingerprint").String(); err == nil {
			nodeInfo.TlsSettings.FingerPrint = fingerprint
		}
		if serverNameData, serverNameExists := tlsSettings.CheckGet("serverName"); serverNameExists {
			if serverName, err := serverNameData.String(); err == nil {
				nodeInfo.TlsSettings.ServerName = serverName
			}
		}
		if verifyPeerData, verifyPeerExists := tlsSettings.CheckGet("verifyPeerCertByName"); verifyPeerExists {
			if verifyPeer, err := verifyPeerData.String(); err == nil {
				nodeInfo.TlsSettings.VerifyPeerCertByName = verifyPeer
			}
		}
		if echConfigList, err := tlsSettings.Get("echConfigList").String(); err == nil {
			nodeInfo.TlsSettings.ECHConfigList = echConfigList
		}
		if peerCert, err := tlsSettings.Get("pinnedPeerCertSha256").String(); err == nil {
			nodeInfo.TlsSettings.PinnedPeerCertSha256 = peerCert
		}
	}

	if realitySettings, ok := securityData.CheckGet("realitySettings"); ok {
		nodeInfo.SecurityType = "reality"
		nodeInfo.RealitySettings = &RealitySettings{}

		if show, err := realitySettings.Get("show").Bool(); err == nil {
			nodeInfo.RealitySettings.Show = show
		}
		if publicKey, err := realitySettings.Get("password").String(); err == nil {
			nodeInfo.RealitySettings.PublicKey = publicKey
		}
		if serverName, err := realitySettings.Get("serverName").String(); err == nil {
			nodeInfo.RealitySettings.ServerName = serverName
		}
		if shortid, err := realitySettings.Get("shortid").String(); err == nil {
			nodeInfo.RealitySettings.ShortId = shortid
		}
		if spiderX, err := realitySettings.Get("spiderX").String(); err == nil {
			nodeInfo.RealitySettings.SpiderX = spiderX
		}
		if fingerprint, err := realitySettings.Get("fingerprint").String(); err == nil {
			nodeInfo.RealitySettings.Fingerprint = fingerprint
		}
		if mldsa65Verify, err := realitySettings.Get("mldsa65Verify").String(); err == nil {
			nodeInfo.RealitySettings.Mldsa65Verify = mldsa65Verify
		}
	}
}

func parseMaskSettingsInto(maskSettings *simplejson.Json, ms **MaskSettings) error {
	hasTCP := false
	hasUDP := false
	var quicParams *QuicParamsSettings
	
	*ms = &MaskSettings{}

	if maskTCP, isOK := maskSettings.CheckGet("tcp"); isOK {
		maskArray, err := maskTCP.Array()
		if err != nil {
			return err
		}
		hasTCP = len(maskArray) > 0
	}

	if maskUDP, isOK := maskSettings.CheckGet("udp"); isOK {
		maskArray, err := maskUDP.Array()
		if err != nil {
			return err
		}
		hasUDP = len(maskArray) > 0
	}

	if qp, isOK := maskSettings.CheckGet("quicParams"); isOK {
		parsed, err := parseQuicParams(qp)
		if err != nil {
			return fmt.Errorf("quicParams: %w", err)
		}
		quicParams = parsed
		(*ms).EnabledQuic = true
	}

	if !hasTCP && !hasUDP && quicParams == nil {
		return nil
	}

	(*ms).Enabled = true

	if hasTCP {
		firstMask := maskSettings.Get("tcp").GetIndex(0)
		parsed, err := parseSingleMask(firstMask)
		if err != nil {
			return fmt.Errorf("tcp mask: %w", err)
		}
		(*ms).TCP = parsed
	}

	if hasUDP {
		firstMask := maskSettings.Get("udp").GetIndex(0)
		parsed, err := parseSingleMask(firstMask)
		if err != nil {
			return fmt.Errorf("udp mask: %w", err)
		}
		(*ms).UDP = parsed
	}

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

	if udpHopData, isOK := qp.CheckGet("udpHop"); isOK {
		hop := &UdpHopSettings{}
		if portsData, portsOK := udpHopData.CheckGet("ports"); portsOK {
			var portStr string
			if s, err := portsData.String(); err == nil {
				portStr = s
			} else if arr, err := portsData.StringArray(); err == nil {
				portStr = strings.Join(arr, ",")
			} else {
				return nil, fmt.Errorf("udpHop.ports: unsupported type, expected string or array")
			}

			portRanges, err := parsePortString(portStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse UdpHop ports: %w", err)
			}

			if len(portRanges) == 0 {
				return nil, fmt.Errorf("no valid UdpHop port ranges found in: %s", portStr)
			}

			hop.Ports = conf.PortList{Range: portRanges}
		}
		if intervalData, intervalOK := udpHopData.CheckGet("interval"); intervalOK {
			from, errFrom := intervalData.Get("from").Int()
			to, errTo := intervalData.Get("to").Int()
			if errFrom == nil && errTo == nil {
				hop.Interval = &Int32RangeSettings{
					From: int32(from),
					To:   int32(to),
				}
			}
		}
		q.UdpHop = hop
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
				return nil, fmt.Errorf("invalid port number in range: %s", parts[0])
			}
			to, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid port number in range: %s", parts[1])
			}
			if from > to {
				return nil, fmt.Errorf("starting port 【%d】 cannot be greater than ending port 【%d】", from, to)
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