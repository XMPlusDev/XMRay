package instance

import (
	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/helper/cert"
	"github.com/xmplusdev/xmray/limiter"
	"github.com/xmplusdev/xmray/node"
)

type Config struct {
	InstanceConfig *InstanceConfig      `mapstructure:"InstanceConfig"`
	RedisConfig    *limiter.RedisConfig `mapstructure:"RedisConfig"`
	ReverbConfig   []*ReverbConfig      `mapstructure:"ReverbConfig"`
	CertConfig     *cert.CertConfig `mapstructure:"CertConfig"`
	ApiConfig      *api.Config `mapstructure:"ApiConfig"`
}

type NodesConfig struct {
	ApiConfig *api.Config `mapstructure:"ApiConfig"`
}

type InstanceConfig struct {
	LogConfig          *LogConfig        `mapstructure:"LogConfig"`
	DNSConfig          *DNSConfig        `mapstructure:"DNSConfig"`
	RouteConfig        *RouteConfig      `mapstructure:"RouteConfig"`
	OutboundConfig     *OutboundConfig   `mapstructure:"OutboundConfig"`
	ConnectionConfig   *ConnectionConfig `mapstructure:"ConnectionConfig"`
}

type LogConfig struct {
	Level       string `mapstructure:"Level"`
	AccessPath  string `mapstructure:"AccessPath"`
	ErrorPath   string `mapstructure:"ErrorPath"`
	DNSLog      bool   `mapstructure:"DNSLog"`
	MaskAddress string `mapstructure:"MaskAddress"`
}

type DNSConfig struct {
	Enable   bool   `mapstructure:"Enable"`
	Path     string `mapstructure:"Path"`
	Strategy string `mapstructure:"Strategy"`
}

type RouteConfig struct {
	Enable     bool   `mapstructure:"Enable"`
	Path       string `mapstructure:"Path"`
}

type OutboundConfig struct {
	Enable     bool   `mapstructure:"Enable"`
	Path       string `mapstructure:"Path"`
}

type ReverbConfig struct {
	Enable    bool   `mapstructure:"Enable"`
	Host      string `mapstructure:"Host"`
	AppKey    string `mapstructure:"AppKey"`
	AppSecret string `mapstructure:"AppSecret"`
	UseTLS    bool   `mapstructure:"UseTLS"`
}

type ConnectionConfig struct {
	Handshake    uint32 `mapstructure:"handshake"`
	ConnIdle     uint32 `mapstructure:"connIdle"`
	UplinkOnly   uint32 `mapstructure:"uplinkOnly"`
	DownlinkOnly uint32 `mapstructure:"downlinkOnly"`
	BufferSize   int32  `mapstructure:"bufferSize"`
}

func getDefaultLogConfig() *LogConfig {
	return &LogConfig{
		Level:       "none",
		AccessPath:  "",
		ErrorPath:   "",
		DNSLog:      false,
		MaskAddress: "half",
	}
}

func getDefaultConnectionConfig() *ConnectionConfig {
	return &ConnectionConfig{
		Handshake:    8,
		ConnIdle:     120,
		UplinkOnly:   0,
		DownlinkOnly: 0,
		BufferSize:   64,
	}
}

func getDefaultControllerConfig() *node.Config {
	return &node.Config{}
}

func buildControllerConfig(cfg *Config) *node.Config {
	c := getDefaultControllerConfig()
	c.CertConfig = cfg.CertConfig
	if cfg.InstanceConfig != nil && cfg.InstanceConfig.DNSConfig != nil {
		c.DNSConfig = &node.DNSConfig{
			Enable:   cfg.InstanceConfig.DNSConfig.Enable,
			Strategy: cfg.InstanceConfig.DNSConfig.Strategy,
		}
	}
	return c
}
