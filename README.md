# XMPlus
Backend for NuxtJs version of XMPlus management panel

#### Config directory
```
cd /etc/XMPlus
```

### Onclick XMPlus backennd Install
```
bash <(curl -Ls https://raw.githubusercontent.com/XMPlusDev/XMPlusServer/script/install.sh)
```

### /etc/XMPlus/config.yml
```
Log:
  Level: none # Log level: none, error, warning, info, debug 
  AccessPath: # /etc/XMPlus/access.Log
  ErrorPath: # /etc/XMPlus/error.log
  DNSLog: false  # / true or false Whether to enable DNS query log, for example: DOH//doh.server got answer: domain.com -> [ip1, ip2] 2.333ms 
  MaskAddress: half # half, full, quater
DnsConfigPath:  /etc/XMPlus/dns.json   #https://xtls.github.io/config/dns.html
RouteConfigPath: # /etc/XMPlus/route.json   #https://xtls.github.io/config/routing.html
InboundConfigPath: # /etc/XMPlus/inbound.json  #https://xtls.github.io/config/inbound.html#inboundobject
OutboundConfigPath: # /etc/XMPlus/outbound.json   #https://xtls.github.io/config/outbound.html
ConnectionConfig:
  Handshake: 8 
  ConnIdle: 120 
  UplinkOnly: 0 
  DownlinkOnly: 0 
  BufferSize: 64
Nodes:
  -
    ApiConfig:
      ApiHost: "https://www.xyz.com"
      ApiKey: "123"
      NodeID: 1
      Timeout: 30 
    ControllerConfig:
      EnableDNS: true # Use custom DNS config, Please ensure that you set the dns.json well
      DNSStrategy: AsIs # AsIs, UseIP, UseIPv4, UseIPv6
      CertConfig:
        Email: author@cert.xyz                    # Required when Cert Mode is not none
        CertFile: /etc/XMPlus/node1.cert.xyz.crt  # Required when Cert Mode is file
        KeyFile: /etc/XMPlus/node1..key   # Required when Cert Mode is file
        Provider: cloudflare                        # Required when Cert Mode is dns
        CertEnv:                                    # Required when Cert Mode is dns
          CLOUDFLARE_EMAIL:                         # Required when Cert Mode is dns
          CLOUDFLARE_API_KEY:                       # Required when Cert Mode is dns
      EnableFallback: false # Only support for Trojan and Vless
      FallBackConfigs:  # Support multiple fallbacks
        - SNI: # TLS SNI(Server Name Indication), Empty for any
          Alpn: # Alpn, Empty for any
          Path: # HTTP PATH, Empty for any
          Dest: 80 # Required, Destination of fallback, check https://xtls.github.io/config/features/fallback.html for details.
          ProxyProtocolVer: 0 # Send PROXY protocol version, 0 for disable
      RedisConfig:
        Enable: false # Enable the global ip limit of a user
        Network: tcp # Redis protocol, tcp or unix
        Addr: 127.0.0.1:6379 # Redis server address, or unix socket path
        Username: # Redis username
        Password: # Redis password
        DB: 0 # Redis DB
        Timeout: 10 # Timeout for redis request
```

## XMPlus Panel Server configuration

### Network Settings

#### TCP
```
{
  "encryption": "none",
  "decryption": "none",
  "flow": "xtls-rprx-vision",
  "cipher": "aes-128-gcm",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443-443",
  "sendThroughIP": "0.0.0.0",
  "transportProtocol": {
    "type": "raw",
    "settings": {
      "acceptProxyProtocol": false,
      "header": {
        "type": "none"
      }
    }
  },
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "asis",
    "tcpKeepAliveInterval": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpKeepAliveIdle": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr"
  }
}
```
#### TCP + HTTP
```
{
  "encryption": "none",
  "decryption": "none",
  "cipher": "aes-128-gcm",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443-443",
  "sendThroughIP": "0.0.0.0",
  "transportProtocol": {
    "type": "raw",
    "settings": {
      "acceptProxyProtocol": false,
      "header": {
        "type": "http",
        "request": {
          "path": ["/"],
          "headers": {
            "Host": ["www.baidu.com", "www.bing.com"]
          }
        }
      }
    }
  },
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "asis",
    "tcpKeepAliveInterval": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpKeepAliveIdle": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr"
  }
}
```
####  WS
```
{
  "encryption": "none",
  "decryption": "none",
  "cipher": "aes-128-gcm",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443-443",
  "sendThroughIP": "0.0.0.0",
  "transportProtocol": {
    "type": "ws",
    "settings": {
      "acceptProxyProtocol": false,
      "host": "xmplus.dev",
      "path": "/",
      "heartbeat": 60,
      "custom_host": "xmplus.dev"
    }
  },
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "asis",
    "tcpKeepAliveInterval": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpKeepAliveIdle": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr"
  }
}
```

####  GRPC
```
{
  "encryption": "none",
  "decryption": "none",
  "cipher": "aes-128-gcm",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443-443",
  "sendThroughIP": "0.0.0.0",
  "acceptProxyProtocol": false,
  "transportProtocol": {
    "type": "grpc",
    "settings": {
      "servicename": "xmplus",
      "authority": "xmplus.dev",
      "user_agent": "",
      "initial_windows_size": 0,
      "idle_timeout": 0,
      "health_check_timeout": 0,
      "permit_without_stream": false
    }
  },
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "asis",
    "tcpKeepAliveInterval": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpKeepAliveIdle": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr"
  }
}
```

####  HTTPUPGRADE
```
{
  "encryption": "none",
  "decryption": "none",
  "cipher": "aes-128-gcm",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443-443",
  "sendThroughIP": "0.0.0.0",
  "transportProtocol": {
    "type": "httpupgrade",
    "settings": {
      "acceptProxyProtocol": false,
      "host": "xmplus.dev",
      "path": "/",
      "custom_host": "xmplus.dev"
    }
  },
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "asis",
    "tcpKeepAliveInterval": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpKeepAliveIdle": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr"
  }
}
```

####  XHTTP
```
{
  "encryption": "none",
  "decryption": "none",
  "cipher": "aes-128-gcm",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443-443",
  "sendThroughIP": "0.0.0.0",
  "transportProtocol": {
    "type": "xhttp",
    "settings": {
      "host": "xmplus.dev",
      "mode": "packet-up",
      "path": "/",
      "extra": {
        "noSSEHeader": false,
        "scMaxBufferedPosts": 30,
        "scMaxEachPostBytes": 1000000,
        "scStreamUpServerSecs": "20-80",
        "xPaddingBytes": "100-1000"
      },
      "custom_host": "xmplus.dev"
    }
  },
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "asis",
    "tcpKeepAliveInterval": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpKeepAliveIdle": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr"
  }
}
```

####  KCP
```
{
  "encryption": "none",
  "decryption": "none",
  "cipher": "aes-128-gcm",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443-443",
  "sendThroughIP": "0.0.0.0",
  "transportProtocol": {
    "type": "kcp",
    "settings": {
      "congestion": false,
      "mtu": 1350
    }
  },
  "maskSettings": {
    "udp": [
      {
        "type": "xicmp",
        "settings": {
          "id": "1234",
          "listenIp": "0.0.0.0"
        }
      }
    ]
  },
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "asis",
    "tcpKeepAliveInterval": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpKeepAliveIdle": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr"
  }
}
```

####  HYSTERIA
```
{
  "encryption": "none",
  "decryption": "none",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443-443",
  "sendThroughIP": "0.0.0.0",
  "transportProtocol": {
    "type": "hysteria",
    "settings": {
      "version": 2
    }
  },
  "maskSettings": {
    "udp": [
      {
        "type": "salamander",
        "settings": {
          "password": "your-password-here"
        }
      }
    ]
  },
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "asis",
    "tcpKeepAliveInterval": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpKeepAliveIdle": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr"
  }
}
```

### Security Settings

#### TLS
```
{
  "tlsSettings": {
    "allowInsecure": false,
    "alpn": ["h2", "http/1.1"],
    "certMode": "http",
    "certDomainName": "xmplus.dev",
    "fragment": "1,40-60,30-50",
    "serverName": "google.com",
    "fingerprint": "chrome",
    "curvePreferences": ["X25519", "X25519MLKEM768"],
    "rejectUnknownSni": false,
    "verifyPeerCertByName": "google.com",
    "pinnedPeerCertSha256": "",
    "echServerKeys": "",
    "echConfigList": ""
  }
}
```
#### REALITY

```
{
  "realitySettings": {
    "target": "www.microsoft.com:443",
    "show": false,
    "shortids": ["6ba85179e30d4fc2"],
    "password": "u2Yirzjxx5R5miuJ-Od8CL4gAiCWj-65WOF2mSVyUz4",
    "privateKey": "sBFSY3OzslfjR2VcSHaQG-6GASrH5YswYyqBR-1m3Vc",
    "fingerprint": "chrome",
    "serverNames": ["www.microsoft.com"],
    "proxyprotocol": 0,
    "mldsa65Seed": "",
    "mldsa65Verify": "",
    "spiderX": "",
    "minClientVer": "",
    "maxClientVer": "",
    "maxTimeDiff": 0
  }
}
```

# XMPlus Commands Reference

## Basic Operations

| Command | Description |
|---------|-------------|
| `XMPlus` | Show menu (more features) |
| `XMPlus start` | Start XMPlus |
| `XMPlus stop` | Stop XMPlus |
| `XMPlus restart` | Restart XMPlus |
| `XMPlus status` | View XMPlus status |

## Service Management

| Command | Description |
|---------|-------------|
| `XMPlus enable` | Enable XMPlus auto-start |
| `XMPlus disable` | Disable XMPlus auto-start |

## Logging & Configuration

| Command | Description |
|---------|-------------|
| `XMPlus log` | View XMPlus logs |
| `XMPlus config` | Show configuration file content |

## Installation & Updates

| Command | Description |
|---------|-------------|
| `XMPlus install` | Install XMPlus |
| `XMPlus uninstall` | Uninstall XMPlus |
| `XMPlus update` | Update XMPlus |
| `XMPlus update vx.x.x` | Update XMPlus to specific version |
| `XMPlus version` | View XMPlus version |

## Key Generation & Utilities

| Command | Description |
|---------|-------------|
| `XMPlus warp` | Generate Cloudflare WARP account |
| `XMPlus x25519` | Generate key pair for X25519 key exchange (REALITY, VLESS Encryption) |
| `XMPlus mldsa65` | Generate key pair for ML-DSA-65 post-quantum signature (REALITY) |
| `XMPlus mlkem768` | Generate key pair for ML-KEM-768 post-quantum key exchange (VLESS Encryption) |
| `XMPlus vlessenc` | Generate decryption/encryption JSON pair (VLESS Encryption) |
| `XMPlus obtain` | Generate SSL/TLS certificate for domain name |
| `XMPlus renew` | Renew SSL/TLS certificate for domain name |
| `XMPlus ping` | Ping a domain with TLS handshake |
| `XMPlus ech` | Generate ECH keys with default or custom server name |
| `XMPlus hash` | Calculate hash for specific certificate |
| `XMPlus generate` | Generate self-signed TLS certificates for testing and production use |
