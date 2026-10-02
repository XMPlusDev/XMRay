package cert

type CertConfig struct {
	Provider  string                      `mapstructure:"Provider"`
	Email     string                      `mapstructure:"Email"`
	CertEnv   map[string]string           `mapstructure:"CertEnv"`
	CertFile  string                      `mapstructure:"CertFile"`
	KeyFile   string                      `mapstructure:"KeyFile"`
	Providers map[string]*ProviderConfig  `mapstructure:"Providers"`
}

// ProviderConfig holds credentials for a specific DNS provider.
// Used when nodes on the same server use different DNS providers.
type ProviderConfig struct {
	CertEnv map[string]string `mapstructure:"CertEnv"`
}

type LegoCMD struct {
	C    *CertConfig
	path string
}
