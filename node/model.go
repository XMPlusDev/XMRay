package node

import (
	"github.com/xmplusdev/xmplus-server/helper/cert"
	"github.com/xmplusdev/xmplus-server/helper/limiter"
)

type Config struct {
	CertConfig              *cert.CertConfig     `mapstructure:"CertConfig"`
	EnableFallback          bool                 `mapstructure:"EnableFallback"`
	FallBackConfigs         []*FallBackConfig    `mapstructure:"FallBackConfigs"`
	EnableDNS               bool                 `mapstructure:"EnableDNS"`
	DNSStrategy             string               `mapstructure:"DNSStrategy"`
	RedisConfig             *limiter.RedisConfig `mapstructure:"RedisConfig"`
}

type FallBackConfig struct {
	SNI              string `mapstructure:"SNI"`
	Alpn             string `mapstructure:"Alpn"`
	Path             string `mapstructure:"Path"`
	Dest             string `mapstructure:"Dest"`
	ProxyProtocolVer uint64 `mapstructure:"ProxyProtocolVer"`
}
