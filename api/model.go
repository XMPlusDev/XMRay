package api

import (
	"encoding/json"
)

const (
	SubscriptionNotModified = "subscriptions not modified"
	NodeNotModified         = "node not modified"
	RuleNotModified         = "rules not modified"
)

type Config struct {
	APIHost  string `mapstructure:"ApiHost"`
	NodeID   int    `mapstructure:"NodeID"`
	ServerID int    `mapstructure:"ServerID"`
	Key      string `mapstructure:"ApiKey"`
	Timeout  int    `mapstructure:"Timeout"`
}

type ServerNode struct {
	NodeID int `json:"node_id"`
}

type ServerNodesResponse struct {
	Nodes        []*ServerNode `json:"nodes"`
	PollInterval int           `json:"poll_interval"`
	Version      int           `json:"api_version"`
}

type Response struct {
	Data json.RawMessage `json:"data"`
}

type PostData struct {
	Key  string      `json:"key"`
	Data interface{} `json:"data"`
}

type serverConfig struct {
	server         `json:"server"`
	transitServer  `json:"transit_server"`
	UpdateInterval int      `json:"update_interval"`
	IgnoreIPs      []string `json:"ignore_ips"`
}

type server struct {
	Type             string           `json:"type"`
	IP               string           `json:"ip"`
	RelayNodeId      int              `json:"transit_server_id"`
	RelayType        int              `json:"transit_server_type"`
	ServerKey        string           `json:"server_key"`
	Speedlimit       int              `json:"speed_limit"`
	NetworkSettings  *json.RawMessage `json:"transportSettings"`
	SecuritySettings *json.RawMessage `json:"securitySettings"`
	Rules            *json.RawMessage `json:"rules"`
}

type transitServer struct {
	RType             string           `json:"type"`
	NodeId            int              `json:"server_id"`
	RAddress          string           `json:"address"`
	RPort             string           `json:"server_port"`
	RServerKey        string           `json:"server_key"`
	RNetworkSettings  *json.RawMessage `json:"transportSettings"`
	RSecuritySettings *json.RawMessage `json:"securitySettings"`
}

type SubscriptionResponse struct {
	Data json.RawMessage `json:"subscriptions"`
}

type Traffic struct {
	Id       int   `json:"subscription_id"`
	Upload   int64 `json:"u"`
	Download int64 `json:"d"`
}

type AliveIP struct {
	Id int    `json:"subscription_id"`
	IP string `json:"ip"`
}

type Subscription struct {
	Id         int    `json:"id"`
	Email      string `json:"email"`
	UUID       string `json:"uuid"`
	Passwd     string `json:"passwd"`
	Speedlimit int    `json:"speed_limit"`
	Iplimit    int    `json:"ip_limit"`
}

type BlockingRules struct {
	Domain   []string
	IP       []string
	Port     string
	Protocol []string
}

type TlsSettings struct {
	CertMode             string
	CertDomainName       string
	CertEmail            string
	ServerName           string
	FingerPrint          string
	CurvePreferences     []string
	RejectUnknownSni     bool
	VerifyPeerCertByName string
	Alpn                 []string
	CipherSuites         string
	MinVersion           string
	MaxVersion           string
	ECHServerKeys        string
	ECHConfigList        string
	PinnedPeerCertSha256 string

	DnsProvider string
	CertFile    string
	KeyFile     string
}

type RealitySettings struct {
	Dest         json.RawMessage
	Show         bool
	MinClientVer string
	MaxClientVer string
	MaxTimeDiff  uint64
	Xver         uint64
	ServerNames  []string
	ShortIds     []string
	Mldsa65Seed  string
	PrivateKey   string

	ShortId       string
	SpiderX       string
	ServerName    string
	Fingerprint   string
	PublicKey     string
	Mldsa65Verify string
}

type MaskSettings struct {
	Enabled     bool
	EnabledQuic bool
	TCP         []MaskEntry
	UDP         []MaskEntry
	QuicParams  *QuicParamsSettings
}

type MaskEntry struct {
	Type     string
	Settings *json.RawMessage
}

type QuicParamsSettings struct {
	Congestion                  string
	Debug                       bool
	BbrProfile                  string
	BrutalUp                    string
	BrutalDown                  string
	InitStreamReceiveWindow     uint64
	MaxStreamReceiveWindow      uint64
	InitConnectionReceiveWindow uint64
	MaxConnectionReceiveWindow  uint64
	MaxIdleTimeout              int64
	KeepAlivePeriod             int64
	DisablePathMTUDiscovery     bool
	MaxIncomingStreams          int64
	BrutalDisableLossCompensation bool
	DisableChromeParrot           bool
	DisableGSO                    bool 
	DisableStatelessReset         bool  
}

type SocketSettings struct {
	Enabled              bool
	TCPKeepAliveInterval int32
	TCPKeepAliveIdle     int32
	TCPUserTimeout       int32
	TCPMaxSeg            int32
	TcpMptcp             bool
	TCPWindowClamp       int32
	DomainStrategy       string
	TcpCongestion        string
	AcceptProxyProtocol  bool
	V6only               bool
	TFO                  interface{}
	TrustedXForwardedFor []string
}

type XhttpSettings struct {
	Host                 string
	Path                 string
	Mode                 string
	NoSSEHeader          bool
	ScMaxEachPostBytes   int32
	ScStreamUpServerSecs string
	ScMaxBufferedPosts   int64
	XPaddingBytes        string

	XPaddingObfsMode  bool
	XPaddingMethod    string
	XPaddingPlacement string
	XPaddingKey       string
	XPaddingHeader    string
	UplinkHTTPMethod  string
	SeqPlacement      string
	SeqKey            string

	SessionIDPlacement string
	SessionIDKey       string
	SessionIDTable     string
	SessionIDLength    string

	UplinkDataPlacement string
	UplinkDataKey       string
	UplinkChunkSize     string

	Xmux XmuxConfig `json:"xmux"`
}

type XmuxConfig struct {
	MaxConcurrency   string
	MaxConnections   string
	CMaxReuseTimes   string
	HMaxRequestTimes string
	HMaxReusableSecs string
	HKeepAlivePeriod int64
}

type FinalRuleSettings struct {
	Action     string
	Network    string
	Port       string
	IP         []string
	BlockDelay string
}

type RawSettings struct {
	Flow                string
	Header              json.RawMessage
	AcceptProxyProtocol bool
}

type WsSettings struct {
	Host                string
	Path                string
	HeartbeatPeriod     uint32
	AcceptProxyProtocol bool
}

type HttpSettings struct {
	Host                string
	Path                string
	AcceptProxyProtocol bool
}

type GrpcSettings struct {
	ServiceName         string
	Authority           string
	UserAgent           string
	WindowsSize         int32
	IdleTimeout         int32
	HealthCheckTimeout  int32
	PermitWithoutStream bool
}

type KcpSettings struct {
	Mtu uint32
	Tti uint32
}

type HysteriaSettings struct {
	Version int32
}

type FallbackConfig struct {
	SNI              string `json:"sni"`
	Alpn             string `json:"alpn"`
	Path             string `json:"path"`
	Dest             string `json:"dest"`
	ProxyProtocolVer uint64 `json:"xver"`
}

type NodeInfo struct {
	NodeType         string
	NodeID           int
	RelayNodeID      int
	RelayType        int
	SpeedLimit       uint64
	IgnoreIPs        []string
	UpdateTime       int
	Sniffing         bool
	ListeningIP      string
	ListeningPort    string
	SendThroughIP    string
	Cipher           string
	Flow             string
	ServerKey        string
	Decryption       string
	SecurityType     string
	NetworkType      string
	KcpSettings      *KcpSettings
	GrpcSettings     *GrpcSettings
	RawSettings      *RawSettings
	HttpSettings     *HttpSettings
	WsSettings       *WsSettings
	XhttpSettings    *XhttpSettings
	SocketSettings   *SocketSettings
	RealitySettings  *RealitySettings
	TlsSettings      *TlsSettings
	RelayNodeInfo    *RelayNodeInfo
	BlockingRules    *BlockingRules
	MaskSettings     *MaskSettings
	HysteriaSettings *HysteriaSettings
	FinalRules       []FinalRuleSettings
	FallbackConfigs  []FallbackConfig
}

type RelayNodeInfo struct {
	NodeType         string
	NodeID           int
	Address          string
	Port             uint16
	ListeningPort    uint16
	SendThroughIP    string
	SecurityType     string
	NetworkType      string
	Cipher           string
	Flow             string
	ServerKey        string
	Encryption       string
	KcpSettings      *KcpSettings
	GrpcSettings     *GrpcSettings
	RawSettings      *RawSettings
	HttpSettings     *HttpSettings
	WsSettings       *WsSettings
	XhttpSettings    *XhttpSettings
	SocketSettings   *SocketSettings
	RealitySettings  *RealitySettings
	TlsSettings      *TlsSettings
	MaskSettings     *MaskSettings
	HysteriaSettings *HysteriaSettings
	FinalRules       []FinalRuleSettings
}

type SubscriptionInfo struct {
	Id         int
	Email      string
	UUID       string
	Passwd     string
	SpeedLimit uint64
	IPLimit    int
}

type OnlineIP struct {
	Id int    `json:"subscription_id"`
	IP string `json:"ip"`
}

type SubscriptionTraffic struct {
	Id       int   `json:"subscription_id"`
	Upload   int64 `json:"u"`
	Download int64 `json:"d"`
}

type ServerStatus struct {
	CPU         float64 `json:"cpu"`
	MemUsed     uint64  `json:"mem"`
	MemTotal    uint64  `json:"mem_total"`
	SwapUsed    uint64  `json:"swap"`
	SwapTotal   uint64  `json:"swap_total"`
	DiskUsed    uint64  `json:"disk"`
	DiskTotal   uint64  `json:"disk_total"`
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	NetInSpeed  float64 `json:"net_in"`
	NetOutSpeed float64 `json:"net_out"`
	Uptime      uint64  `json:"uptime"`
}

// ServerStatusPayload is the Reverb envelope for a server_status event.
// server_id identifies the machine (matches ApiConfig.ServerID / machines.id).
type ServerStatusPayload struct {
	ServerID int           `json:"server_id"`
	Data     *ServerStatus `json:"data"`
}
