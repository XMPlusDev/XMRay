package node

import (
	"github.com/xmplusdev/xmray/helper/cert"
)

type Config struct {
	CertConfig           *cert.CertConfig `mapstructure:"CertConfig"`
	DNSConfig            *DNSConfig       `mapstructure:"DNSConfig"`
	DisableServerMonitor bool             `mapstructure:"DisableServerMonitor"`
	DisableNodeMonitor   bool             `mapstructure:"DisableNodeMonitor"`
}

type DNSConfig struct {
	Enable           bool   `mapstructure:"Enable"`
	Strategy         string `mapstructure:"Strategy"`
}

type FallBackConfig struct {
	SNI              string `mapstructure:"SNI"`
	Alpn             string `mapstructure:"Alpn"`
	Path             string `mapstructure:"Path"`
	Dest             string `mapstructure:"Dest"`
	ProxyProtocolVer uint64 `mapstructure:"ProxyProtocolVer"`
}
