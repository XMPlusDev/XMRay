package limiter

type RedisConfig struct {
	Enable   bool   `mapstructure:"Enable"`
	Network  string `mapstructure:"Network"` // tcp or unix
	Addr     string `mapstructure:"Addr"`    // host:port, or /path/to/unix.sock
	Username string `mapstructure:"Username"`
	Password string `mapstructure:"Password"`
	DB       int    `mapstructure:"DB"`
	Timeout  int    `mapstructure:"Timeout"`
}
