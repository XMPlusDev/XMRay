# XMRay
Backend for NuxtJs version of XMPlus management panel

#### Config directory
```
cd /etc/XMRay
```

### Onclick XMRay backennd Install
```
bash <(curl -Ls https://raw.githubusercontent.com/XMRayDev/XMRayServer/script/install.sh)
```

### /etc/XMRay/config.yml
```
Log:
  Level: none # Log level: none, error, warning, info, debug 
  AccessPath: # /etc/XMRay/access.Log
  ErrorPath: # /etc/XMRay/error.log
  DNSLog: false  # / true or false Whether to enable DNS query log, for example: DOH//doh.server got answer: domain.com -> [ip1, ip2] 2.333ms 
  MaskAddress: half # half, full, quater
DnsConfigPath:  /etc/XMRay/dns.json   #https://xtls.github.io/config/dns.html
RouteConfigPath: # /etc/XMRay/route.json   #https://xtls.github.io/config/routing.html
InboundConfigPath: # /etc/XMRay/inbound.json  #https://xtls.github.io/config/inbound.html#inboundobject
OutboundConfigPath: # /etc/XMRay/outbound.json   #https://xtls.github.io/config/outbound.html
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
        CertFile: /etc/XMRay/node1.cert.xyz.crt  # Required when Cert Mode is file
        KeyFile: /etc/XMRay/node1..key   # Required when Cert Mode is file
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
      "host": "tld.dev",
      "path": "/",
      "heartbeat": 60,
      "custom_host": "tld.dev"
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
      "servicename": "tld",
      "authority": "tld.dev",
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
      "host": "tld.dev",
      "path": "/",
      "custom_host": "tld.dev"
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
      "host": "tld.dev",
      "mode": "packet-up",
      "path": "/",
      "extra": {
        "noSSEHeader": false,
        "scMaxBufferedPosts": 30,
        "scMaxEachPostBytes": 1000000,
        "scStreamUpServerSecs": "20-80",
        "xPaddingBytes": "100-1000"
      },
      "custom_host": "tld.dev"
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

### Mask Settings

`maskSettings` is optional and applies transport-level obfuscation. All three fields (`tcp`, `udp`, `quicParams`) are optional and can be used independently or together.

#### TCP mask types: `header-custom`, `fragment`, `sudoku`
#### UDP mask types: `header-custom`, `header-dns`, `header-dtls`, `header-srtp`, `header-utp`, `header-wechat`, `header-wireguard`, `mkcp-original`, `mkcp-aes128gcm`, `noise`, `salamander`, `sudoku`, `xdns`, `xicmp`

```json
{
  "maskSettings": {
    "tcp": [
      {
        "type": "fragment",
        "settings": {
          "packets": "tlshello",
          "length": { "from": 100, "to": 200 },
          "delay": { "from": 10, "to": 20 },
          "maxSplit": { "from": 0, "to": 0 }
        }
      }
    ],
    "udp": [
      {
        "type": "noise",
        "settings": {
          "reset": { "from": 0, "to": 0 },
          "noise": [
            {
              "type": "str",
              "packet": "GET / HTTP/1.1\r\n",
              "rand": { "from": 0, "to": 0 },
              "delay": { "from": 10, "to": 50 }
            }
          ]
        }
      }
    ],
    "quicParams": {
      "congestion": "bbr",
      "debug": false,
	  "bbrProfile": "standard",
      "brutalUp": "100mbps",
      "brutalDown": "100mbps",
      "udpHop": {
        "ports": [443, 8443],
        "interval": { "from": 10, "to": 30 }
      },
      "initStreamReceiveWindow": 8388608,
      "maxStreamReceiveWindow": 8388608,
      "initConnectionReceiveWindow": 20971520,
      "maxConnectionReceiveWindow": 20971520,
      "maxIdleTimeout": 30,
      "keepAlivePeriod": 10,
      "disablePathMTUDiscovery": false,
      "maxIncomingStreams": 100
    }
  }
}
```

`quicParams` fields:

| Field | Type | Description |
|---|---|---|
| `congestion` | string | Congestion control algorithm, e.g. `"bbr"`, `"cubic"` |
| `debug` | bool | Enable debug mode |
| `bbrProfile` | string | Congestion control algorithm, e.g. `"conservative"`, `"standard"`, `"aggressive"` |
| `brutalUp` | string | Upload bandwidth for brutal congestion, e.g. `"100mbps"`, `"1gbps"` |
| `brutalDown` | string | Download bandwidth for brutal congestion |
| `udpHop.ports` | array/string | Port list for UDP hopping |
| `udpHop.interval` | object | Hop interval range in seconds `{ "from": N, "to": N }` |
| `initStreamReceiveWindow` | uint64 | Initial stream receive window size (bytes) |
| `maxStreamReceiveWindow` | uint64 | Max stream receive window size (bytes) |
| `initConnectionReceiveWindow` | uint64 | Initial connection receive window size (bytes) |
| `maxConnectionReceiveWindow` | uint64 | Max connection receive window size (bytes) |
| `maxIdleTimeout` | int64 | Max idle timeout in seconds |
| `keepAlivePeriod` | int64 | Keep-alive period in seconds |
| `disablePathMTUDiscovery` | bool | Disable path MTU discovery |
| `maxIncomingStreams` | int64 | Max number of incoming streams |

### Security Settings

#### TLS
```
{
  "tlsSettings": {
    "allowInsecure": false,
    "alpn": ["h2", "http/1.1"],
    "certMode": "http",
    "certDomainName": "tld.dev",
    "fragment": "1,40-60,30-50",
    "serverName": "google.com",
    "fingerprint": "chrome",
    "curvePreferences": ["X25519", "X25519MLKEM768"],
    "rejectUnknownSni": false,
    "verifyPeerCertByName": "google.com",
    "pinnedPeerCertSha256": "",
    "echServerKeys": "",
    "echConfigList": ""
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
  }
}
```

# XMRay Commands Reference

## Basic Operations

| Command | Description |
|---------|-------------|
| `XMRay` | Show menu (more features) |
| `XMRay start` | Start XMRay |
| `XMRay stop` | Stop XMRay |
| `XMRay restart` | Restart XMRay |
| `XMRay status` | View XMRay status |

## Service Management

| Command | Description |
|---------|-------------|
| `XMRay enable` | Enable XMRay auto-start |
| `XMRay disable` | Disable XMRay auto-start |

## Logging & Configuration

| Command | Description |
|---------|-------------|
| `XMRay log` | View XMRay logs |
| `XMRay config` | Show configuration file content |

## Installation & Updates

| Command | Description |
|---------|-------------|
| `XMRay install` | Install XMRay |
| `XMRay uninstall` | Uninstall XMRay |
| `XMRay update` | Update XMRay |
| `XMRay update vx.x.x` | Update XMRay to specific version |
| `XMRay version` | View XMRay version |

## Key Generation & Utilities

| Command | Description |
|---------|-------------|
| `XMRay warp` | Generate Cloudflare WARP account |
| `XMRay x25519` | Generate key pair for X25519 key exchange (REALITY, VLESS Encryption) |
| `XMRay mldsa65` | Generate key pair for ML-DSA-65 post-quantum signature (REALITY) |
| `XMRay mlkem768` | Generate key pair for ML-KEM-768 post-quantum key exchange (VLESS Encryption) |
| `XMRay vlessenc` | Generate decryption/encryption JSON pair (VLESS Encryption) |
| `XMRay obtain` | Generate SSL/TLS certificate for domain name |
| `XMRay renew` | Renew SSL/TLS certificate for domain name |
| `XMRay ping` | Ping a domain with TLS handshake |
| `XMRay ech` | Generate ECH keys with default or custom server name |
| `XMRay hash` | Calculate hash for specific certificate |
| `XMRay generate` | Generate self-signed TLS certificates for testing and production use |