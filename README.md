# XMRay
XRay-core server for NuxtJs version of XMPlus management panel

#### Config directory
```
cd /etc/XMRay
```

### Onclick XMRay backend Install
```
bash <(curl -Ls https://raw.githubusercontent.com/XMPlusDev/XMRay/script/install.sh)
```

### /etc/XMRay/config.yml
```
ApiConfig:                                        # API Configuraion
  ApiHost: "https://api.xyz.com"                  # Panel api host address
  ApiKey: "123"                                   # Server api key from admin general settigs 
  ServerID: 1                                     # Important: The id of the server and not node id.
  Timeout: 30                                     # Connection time out. Cannot be higer than api update interval.
CertConfig:                                       # Cert config for when cert mode is dns
  Provider: cloudflare
  Providers:
    cloudflare:                                   # Provider name. eg, cloudflare. Set in panel tlsSettings - dnsProvider https://go-acme.github.io/lego/dns/index.html
      CertEnv:
        CLOUDFLARE_DNS_API_TOKEN: x               # Cert Provider environment variables.
RedisConfig:
  Enable: false                                   # Enable the global ip limit
  Network: tcp                                    # Redis protocol, tcp or unix
  Addr: 127.0.0.1:6379                            # Redis server address, or unix socket path (use your panel api address and redis port)
  Username:                                       # Redis username leave empty
  Password:                                       # Redis password
  DB: 0                                           # Redis DB
  Timeout: 10                                     # Timeout for redis request
ReverbConfig:
  - Enable: false                                 # Enable websocket to trigger real-time subscription and node updates from panel
    Host: "api.xyz.com:443"                       # Reverb REVERB_HOST:REVERB_PORT  in .env for api /home/XMPlusPanel/.env 
    AppKey:                                       # REVERB_APP_KEY in .env for api /home/XMPlusPanel/.env
    AppSecret:                                    # REVERB_APP_SECRET in .env for api /home/XMPlusPanel/.env
    UseTLS: true                                  # Set to true if tls enabled for api
InstanceConfig:                                   # Xray-core instance configuration
  LogConfig:
    Level: none                                   # Log level: none, error, warning, info, debug 
    AccessPath:                                   # /etc/XMRay/access.Log
    ErrorPath:                                    # /etc/XMRay/error.log
    DNSLog: false                                 # true or false Whether to enable DNS query in logs
    MaskAddress: half                             # half, full, quater
  DnsConfig:
    Enable: true                                  # Use custom dns config, ensure that you set the dns.json correctly
    Path: /etc/XMRay/dns.json                     # /etc/XMRay/dns.json      https://xtls.github.io/config/dns.html
    Strategy: AsIs                                # AsIs, UseIP, UseIPv4, UseIPv6
  RouteConfig:
    Enable: false                                 # Use custom route config, ensure that you set the route.json correctly
    Path: /etc/XMRay/route.json                   #/etc/XMRay/route.json     https://xtls.github.io/config/routing.html
  OutboundConfig:
    Enable: false                                 # Use custom outbound config, ensure that you set the outbound.json correctly
    Path: /etc/XMRay/outbound.json                #/etc/XMRay/outbound.json  https://xtls.github.io/config/outbound.html
  ConnectionConfig:                               # Policy config https://xtls.github.io/config/policy.html
    Handshake: 8                                  # Seconds to complete a handshake before the connection is dropped. Xray default: 4
    ConnIdle: 120                                 # Seconds of inactivity before an idle connection is closed (0 = never). Xray default: 300
    UplinkOnly: 0                                 # Seconds to wait after the downlink closes before tearing down the connection (0 = close immediately). Xray default: 2
    DownlinkOnly: 0                               # Seconds to wait after the uplink closes before tearing down the connection (0 = close immediately). Xray default: 5
    BufferSize: 64                                # Per-connection internal buffer size in KB. UDP packets are dropped when full (0 = disabled, use system default)
```

---

## XMPlus Panel Server configuration

### Network Settings


### Fallback Settings (`fallbacks`) 

> **Applies to:** `vless` and `trojan` node types only.

Fallbacks redirect unrecognised or non-matching connections to another local service (e.g. a web server or another proxy). Configured in the panel's **Network Settings** JSON.

```json
{
  "fallbacks": [
    {
      "sni": "",
      "alpn": "",
      "path": "/",
      "dest": "127.0.0.1:80",
      "xver": 0
    },
    {
      "sni": "example.com",
      "alpn": "h2",
      "path": "",
      "dest": "127.0.0.1:8080",
      "xver": 1
    }
  ]
}
```

<details>
<summary><strong>Example of network settings with fallback settings</strong></summary>

```json
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
  "fallbacks": [
    {
      "sni": "example.com",
      "alpn": "h2",
      "path": "",
      "dest": "127.0.0.1:8080",
      "xver": 1
    }
  ]
}

```
</details>


<details>
<summary><strong>fallbacks fields</strong></summary>

| Field | Type | Required | Description |
|---|---|---|---|
| `dest` | string | ✅ | Fallback destination — `"addr:port"` or just `"port"`. Entries without this are skipped. |
| `sni` | string | ❌ | TLS SNI to match. Empty = match any. |
| `alpn` | string | ❌ | ALPN to match (`h2`, `http/1.1`). Empty = match any. |
| `path` | string | ❌ | HTTP path prefix to match. Empty = match any. |
| `xver` | int | ❌ | PROXY Protocol version sent to `dest` — `0` = disabled, `1` or `2`. |

</details>


#### TCP
```json
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
  }
}
```

#### TCP + HTTP
```json
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
  }
}
```

#### WS
```json
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
  }
}
```

#### GRPC
```json
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
  }
}
```

#### HTTPUPGRADE
```json
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
  }
}
```

#### XHTTP

XHTTP is an HTTP/2 and HTTP/3 based transport that splits uplink and downlink traffic into separate HTTP requests. It supports CDN deployments and padding obfuscation to bypass CDN-level traffic detection.

##### Basic configuration

```json
{
  "encryption": "none",
  "decryption": "none",
  "cipher": "aes-128-gcm",
  "sniffing": true,
  "listeningIP": "0.0.0.0",
  "listeningPort": "443",
  "sendThroughIP": "0.0.0.0",
  "transportProtocol": {
    "type": "xhttp",
    "settings": {
      "host": "tld.dev",
      "path": "/",
      "mode": "auto",
      "noSSEHeader": false,
      "scMaxBufferedPosts": 30,
      "scMaxEachPostBytes": 1000000,
      "scStreamUpServerSecs": "20-80",
      "xPaddingBytes": "100-1000"
    }
  }
}
```

##### `mode` values

| Value | Description |
|---|---|
| `auto` | Automatically selects between `packet-up` and `stream-up` based on client capability |
| `packet-up` | Uplink sent as individual HTTP POST requests. Works with most CDNs |
| `stream-up` | Uplink streamed over a single long-lived HTTP connection. Higher performance but requires CDN POST streaming support |
| `stream-one` | Single bidirectional stream. No `downloadSettings` allowed |

##### Core settings

| Field | Type | Default | Description |
|---|---|---|---|
| `host` | string | — | HTTP Host header value |
| `path` | string | `"/"` | HTTP request path |
| `mode` | string | `"auto"` | Transport mode — see table above |
| `noSSEHeader` | bool | `false` | Suppress the `Content-Type: text/event-stream` header on the downlink response |
| `scMaxEachPostBytes` | int | `1000000` | Max bytes per uplink POST request. Only applies in `packet-up` mode |
| `scMaxBufferedPosts` | int | `30` | Max number of POST requests buffered server-side before backpressure |
| `scStreamUpServerSecs` | string | `"20-80"` | Range (seconds) the server keeps a stream-up connection open before rotating. Format: `"min-max"` |
| `xPaddingBytes` | string | `"100-1000"` | Range of random padding bytes added to requests. Format: `"min-max"` |

---

<details>
<summary><strong>Connection multiplexing (xmux)</strong></summary>

`xmux` controls how the underlying HTTP connections are pooled and reused. It is set as a nested `xmux` object inside `settings`. On the client/relay side XMRay automatically wraps it in the required `extra` block, same as the obfuscation fields.

```json
{
  "transportProtocol": {
    "type": "xhttp",
    "settings": {
      "host": "tld.dev",
      "path": "/",
      "mode": "auto",
      "xmux": {
        "maxConcurrency": "16-32",
        "maxConnections": "",
        "cMaxReuseTimes": "0",
        "hMaxRequestTimes": "600-900",
        "hMaxReusableSecs": "1800-3000",
        "hKeepAlivePeriod": 0
      }
    }
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `maxConcurrency` | string | `"1-1"` | Range of concurrent requests allowed per underlying connection before a new one is opened. Format: `"min-max"` or a single value. Cannot be set together with `maxConnections` |
| `maxConnections` | string | none | Range of the max number of underlying connections to keep open. Format: `"min-max"` or a single value. Cannot be set together with `maxConcurrency` |
| `cMaxReuseTimes` | string | none | Range of times an underlying connection can be reused before being replaced. Format: `"min-max"` or a single value |
| `hMaxRequestTimes` | string | `"600-900"` | Range of HTTP/2 or HTTP/3 requests allowed on a connection before it is rotated. Format: `"min-max"` or a single value |
| `hMaxReusableSecs` | string | `"1800-3000"` | Range (seconds) a connection stays reusable before being rotated. Format: `"min-max"` or a single value |
| `hKeepAlivePeriod` | int | `0` | TCP keep-alive period in seconds. `0` uses the system default |

</details>

---

<details>
<summary><strong>Padding obfuscation (CDN detection bypass)</strong></summary>

These fields enable obfuscation of the padding pattern to bypass CDN-level traffic fingerprinting (e.g. blocking of the default `x_padding=XXXX...` query parameter pattern).

> **Important:** All obfuscation fields must match exactly between server and client. Fields are set directly in `settings` on the server side. On the client/relay side XMRay automatically wraps them in the required `extra` block.

```json
{
  "transportProtocol": {
    "type": "xhttp",
    "settings": {
      "host": "tld.dev",
      "path": "/",
      "mode": "auto",
      "scStreamUpServerSecs": "20-80",
      "xPaddingBytes": "100-1000",
      "xPaddingObfsMode": true,
      "xPaddingMethod": "tokenish",
      "xPaddingPlacement": "queryInHeader",
      "xPaddingKey": "_dc",
      "xPaddingHeader": "X-Cache",
      "uplinkHTTPMethod": "POST",
      "sessionIDPlacement": "path",
      "sessionIDKey": "",
      "sessionIDTable": "",
      "sessionIDLength": "",
      "seqPlacement": "path",
      "seqKey": "",
      "uplinkDataPlacement": "",
      "uplinkDataKey": "",
      "uplinkChunkSize": ""
    }
  }
}
```

</details>

<details>
<summary><strong>Obfuscation fields</strong></summary>

| Field | Type | Default | Description |
|---|---|---|---|
| `xPaddingObfsMode` | bool | `false` | Enable padding obfuscation mode. When `true` the padding key, header, and method are customised |
| `xPaddingMethod` | string | `"repeat-x"` | Padding generation method. `"repeat-x"` repeats the character `X`; `"tokenish"` generates a token-like string of mixed characters that resembles a real CDN cache key or auth token |
| `xPaddingPlacement` | string | `"queryInHeader"` | Where the padding value is placed in the request — see placement options below |
| `xPaddingKey` | string | `"x_padding"` | Query parameter name or cookie name used to carry the padding value. Used when `xPaddingPlacement` is `query`, `cookie`, or `queryInHeader` |
| `xPaddingHeader` | string | `"X-Padding"` | HTTP header name used to carry the padding value. Used when `xPaddingPlacement` is `header` or `queryInHeader` |
| `uplinkHTTPMethod` | string | `"POST"` | HTTP method for uplink requests. `"PUT"` and `"PATCH"` are often not blocked by CDNs that block `POST`. `"GET"` is only allowed in `packet-up` mode |
| `sessionIDPlacement` | string | `"path"` | Where the session ID is placed — see placement options below |
| `sessionIDKey` | string | auto | Key name for the session ID when `sessionIDPlacement` is not `path`. Defaults to `x_session` (query/cookie) or `X-Session` (header) |
| `sessionIDTable` | string | UUID | Custom character set used to generate the session ID. Must be ASCII-only. Can be one of the predefined tables (`Base62`, `Base36`, `HEX`, `alphabet`, `base36`, `hex`, `ALPHABET`, `number`) or a custom string of characters. When empty, a standard UUID is used |
| `sessionIDLength` | string | none | Length range of the generated session ID, format `"min-max"` or a single value. Only used when `sessionIDTable` is set. The range must allow for at least ~2.1 billion possible values and `min` must be greater than 0 |
| `seqPlacement` | string | `"path"` | Where the request sequence number is placed — see placement options below |
| `seqKey` | string | auto | Key name for the sequence number when `seqPlacement` is not `path`. Defaults to `x_seq` (query/cookie) or `X-Seq` (header) |
| `uplinkDataPlacement` | string | `"path"` | Where the uplink data chunk is placed — see placement options below |
| `uplinkDataKey` | string | auto | Key name for the uplink data chunk when `uplinkDataPlacement` is not `path`. Defaults to `x_data` (query/cookie) or `X-Data` (header) |
| `uplinkChunkSize` | string | none | Range of random chunk sizes (in bytes) used to split uplink data, format `"min-max"` or a single value. Helps obscure upload patterns from traffic analysis |

</details>

<details>
<summary><strong>Placement options</strong></summary>

| Value | Description |
|---|---|
| `queryInHeader` | Padding key sent as a query parameter **and** the padding value sent in a header. Combines both for maximum CDN compatibility |
| `query` | Sent as a URL query parameter: `?key=value` |
| `cookie` | Sent as an HTTP `Cookie` header: `Cookie: key=value` |
| `header` | Sent as a standalone HTTP header: `Key: value` |
| `path` | (session/seq only) Embedded directly in the URL path |

</details>

<details>
<summary><strong>Example: CDN obfuscation with custom method and session headers</strong></summary>

A configuration that disguises XHTTP traffic as normal CDN cache validation requests:

```json
{
  "transportProtocol": {
    "type": "xhttp",
    "settings": {
      "host": "tld.dev",
      "path": "/assets/bundle",
      "mode": "auto",
      "xPaddingObfsMode": true,
      "xPaddingMethod": "tokenish",
      "xPaddingPlacement": "queryInHeader",
      "xPaddingKey": "_dc",
      "xPaddingHeader": "X-Cache",
      "uplinkHTTPMethod": "PUT",
      "sessionIDPlacement": "header",
      "sessionIDKey": "X-Request-ID",
      "sessionIDTable": "Base62",
      "sessionIDLength": "16-24",
      "seqPlacement": "query",
      "seqKey": "fragment",
      "uplinkDataPlacement": "header",
      "uplinkDataKey": "X-Data",
      "uplinkChunkSize": "500-1000"
    }
  }
}
```

> `uplinkHTTPMethod: "PUT"` requires `mode: "packet-up"` or `mode: "auto"` (auto will use packet-up). It cannot be used with `stream-up` or `stream-one`.

---

##### Recommended key names for obfuscation

Choose names that blend in with real CDN traffic. Some suggestions:

**For `xPaddingKey` (query/cookie):** `_dc`, `cf`, `t`, `ts`, `bust`, `v`, `rev`, `cb`, `cache_key`

**For `xPaddingHeader` (header):** `X-Cache`, `X-CDN-Geo`, `X-Signature`, `X-Request-ID`, `CF-Cache-Status`

**For `sessionIDKey`:** `X-Request-ID`, `X-Client-ID`, `X-Auth-Token`, `sid`, `token`, `visitor_id`

**For `seqKey`:** `chunk`, `fragment`, `part`, `segment`, `offset`, `range`

**For `uplinkDataKey`:** `X-Data`, `payload`, `body`, `blob`, `chunk_data`

</details>

---

#### KCP
```json
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
      "mtu": 1350,
	  "tti": 20,
	  "uplinkCapacity": 5,
      "downlinkCapacity": 20,
	  "cwndMultiplier": 1,
	  "maxSendingWindow": 1350
    }
  }
}
```

#### HYSTERIA
```json
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
  }
}
```

---

### Mask Settings

[FinalMask](https://xtls.github.io/config/transports/finalmask.html)

`maskSettings` is optional and applies transport-level obfuscation. All three fields (`tcp`, `udp`, `quicParams`) are optional and can be used independently or together.

#### TCP mask types: `header-custom`, `fragment`, `sudoku`
#### UDP mask types:  `mkcp-legacy`, `mkcp-original`, `mkcp-aes128gcm`, `noise`, `salamander`, `sudoku`, `xdns`, `xicmp`, `realm`

> **`salamander` packetSize (Gecko mode):** add an optional `"packetSize": { "from": <min>, "to": <max> }` to the `salamander` settings (Hysteria v2.9.2+) to enable the Gecko obfuscator, which fragments packets into randomly-sized chunks within the given byte range to resist traffic analysis. Omit `packetSize` to use the classic Salamander obfuscator. This field must match exactly between server and client.

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
      "initStreamReceiveWindow": 8388608,
      "maxStreamReceiveWindow": 8388608,
      "initConnectionReceiveWindow": 20971520,
      "maxConnectionReceiveWindow": 20971520,
      "maxIdleTimeout": 30,
      "keepAlivePeriod": 10,
      "disablePathMTUDiscovery": false,
      "maxIncomingStreams": 100,
	  "brutalDisableLossCompensation": true,
	  "DisableChromeParrot":true,
	  "disableGSO": false,
	  "disableStatelessReset": false
    }
  }
}
```

<details>
<summary><strong>maskSettings — quicParams fields</strong></summary>

[quicParams](https://xtls.github.io/config/transports/finalmask.html#quicparams)

| Field | Type | Description |
|---|---|---|
| `congestion` | string | Congestion control algorithm, e.g. `"bbr"`, `"cubic"` |
| `debug` | bool | Enable debug mode |
| `bbrProfile` | string | BBR profile: `"conservative"`, `"standard"`, `"aggressive"` |
| `brutalUp` | string | Upload bandwidth for brutal congestion, e.g. `"100mbps"`, `"1gbps"` |
| `brutalDown` | string | Download bandwidth for brutal congestion |
| `initStreamReceiveWindow` | uint64 | Initial stream receive window size (bytes) |
| `maxStreamReceiveWindow` | uint64 | Max stream receive window size (bytes) |
| `initConnectionReceiveWindow` | uint64 | Initial connection receive window size (bytes) |
| `maxConnectionReceiveWindow` | uint64 | Max connection receive window size (bytes) |
| `maxIdleTimeout` | int64 | Max idle timeout in seconds |
| `keepAlivePeriod` | int64 | Keep-alive period in seconds |
| `disablePathMTUDiscovery` | bool | Disable path MTU discovery |
| `maxIncomingStreams` | int64 | Max number of incoming streams |

</details>

---

### Final Rule Settings (`finalRule`)

[FinalRule](https://xtls.github.io/config/outbounds/freedom.html#finalruleobject)

```json
"finalRules": [
  {
    "action": "block",
    "network": "tcp,udp",
    "port": "53,443",
    "ip": ["10.0.0.0/8", "2001:db8::/32"],
    "blockDelay": "30-90"
  }
]
```

<details>
<summary><strong>finalRules fields</strong></summary>

| Field | Type | Description |
|---|---|---|
| `action` | string | Action when rule matches. `"allow"` permits the connection, `"block"` drops it |
| `network` | string | Comma-separated network types: `"tcp"`, `"udp"`, `"tcp,udp"` |
| `port` | string | Port or range to match, e.g. `"53"`, `"443"`, `"8080-9000"`, `"53,443,8080-9000"` |
| `ip` | array | List of IP CIDRs or geo tags, e.g. `"10.0.0.0/8"`, `"geoip:cn"` |
| `blockDelay` | string | Random delay (ms) before dropping when `action` is `"block"`, e.g. `"30-90"`. Omit for immediate drop |

</details>

---

### Socket Settings (`socketSettings`)

[Sockopt](https://xtls.github.io/config/transports/sockopt.html)

Socket-level options applied to the underlying TCP/UDP socket. All fields are optional — omitting a field leaves Xray's default in place.

```json
{
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "AsIs",
    "tcpFastOpen": false,
    "tcpKeepAliveInterval": 0,
    "tcpKeepAliveIdle": 120,
    "tcpUserTimeout": 10000,
    "tcpMaxSeg": 1440,
    "tcpWindowClamp": 600,
    "tcpMptcp": false,
    "tcpCongestion": "bbr",
    "v6only": false,
    "addressPortStrategy": "",
    "trustedXForwardedFor": []
  }
}
```

<details>
<summary><strong>socketSettings fields</strong></summary>

| Field | Type | Default | Scope | Description |
|---|---|---|---|---|
| `acceptProxyProtocol` | bool | `false` | Inbound | Accept PROXY protocol v1/v2 from an upstream load balancer or reverse proxy (e.g. Nginx, HAProxy). Real client IP is read from the PROXY header. TCP-based transports only (`tcp`, `ws`, `httpupgrade`). |
| `domainStrategy` | string | `"AsIs"` | Both | DNS resolution strategy for outbound connections. See strategies table below. |
| `tcpFastOpen` | bool \| int | `false` | Both | Enable TCP Fast Open (TFO). `true` uses OS default queue size; integer sets explicit queue size. Requires kernel ≥ 3.7 (Linux) or Windows 10 1607+. |
| `tcpKeepAliveInterval` | int | `0` | Both | Seconds between TCP keep-alive probes after idle period expires. Set together with `tcpKeepAliveIdle`. |
| `tcpKeepAliveIdle` | int | `0` | Both | Seconds of inactivity before first keep-alive probe. OS default ~7200s on Linux. |
| `tcpUserTimeout` | int | `0` | Both | Milliseconds before aborting connection with unacknowledged data (`TCP_USER_TIMEOUT`). |
| `tcpMaxSeg` | int | `0` | Both | Max TCP segment size in bytes (`TCP_MAXSEG`). Reduce below 1460 when using tunnels to avoid fragmentation. |
| `tcpWindowClamp` | int | `0` | Both | Clamp TCP receive window to this size (`TCP_WINDOW_CLAMP`). |
| `tcpMptcp` | bool | `false` | Both | Enable Multipath TCP. Requires kernel ≥ 5.6 with MPTCP compiled in. |
| `tcpCongestion` | string | `""` | Both | TCP congestion algorithm: `"bbr"`, `"cubic"`, `"reno"`. Must be loaded in kernel (`modprobe tcp_bbr`). |
| `v6only` | bool | `false` | Both | When `true`, IPv6 socket will not accept IPv4-mapped connections (`IPV6_V6ONLY`). |
| `trustedXForwardedFor` | string[] | `[]` | Inbound | Trusted upstream CIDRs for `X-Forwarded-For` header extraction. HTTP-based inbounds only. |
| `addressPortStrategy`  | string | `none` | Inbound | `"none" | "SrvPortOnly" | "SrvAddressOnly" | "SrvPortAndAddress" | "TxtPortOnly" | "TxtAddressOnly" | "TxtPortAndAddress"` Use SRV or TXT records to specify the destination address/port for outbound traffic; the default is none, meaning it's disabled. |

##### `domainStrategy` values

| Value | Description |
|---|---|
| `"AsIs"` | Use domain name as-is; let the OS resolve it. Default. |
| `"UseIP"` | Resolve domain to IP using Xray's internal DNS before connecting. |
| `"UseIPv4"` | Resolve and force IPv4. |
| `"UseIPv6"` | Resolve and force IPv6. |
| `"UseIPv4v6"` | Resolve and prefer IPv4, fall back to IPv6. |
| `"UseIPv6v4"` | Resolve and prefer IPv6, fall back to IPv4. |

##### Notes

**`acceptProxyProtocol` vs `trustedXForwardedFor`** — `acceptProxyProtocol` reads the real IP from a binary PROXY protocol header at the TCP layer (Nginx `proxy_protocol on`). `trustedXForwardedFor` reads it from an HTTP header at the application layer (Nginx `proxy_set_header X-Forwarded-For`). Use the one that matches your reverse proxy configuration.

**`tcpKeepAliveInterval` and `tcpKeepAliveIdle`** — both must be set together for keep-alive to behave predictably.

**`tcpFastOpen`** — must be enabled on both client and server. Also requires `net.ipv4.tcp_fastopen=3` (`sysctl -w net.ipv4.tcp_fastopen=3`).

**`interface`** — the named interface must exist when the node starts. Existing connections are not migrated if it goes down and comes back.

**`dialerProxy`** — the referenced outbound tag must exist in the Xray config. Circular references cause a connection loop.

</details>

---

### Security Settings

> **NOTE:** `socketSettings`, `maskSettings` and `finalRules` are optional. You can choose not to add them to the configuration.

#### NONE
```json
{
  "none": []
}
```

#### TLS
```json
{
  "tlsSettings": {
    "alpn": ["h2", "http/1.1"],
    "certMode": "http",
	"certEmail": "author@cert.xyz",
    "certDomainName": "tld.dev",
	"dnsProvider": "cloudflare",
	"certFile": "/etc/XMRay/node3.crt",
    "keyFile": "/etc/XMRay/node3.key",
	"cipherSuites": "",
	"minVersion": "1.2",
	"maxVersion": "1.3",
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

<details>
<summary><strong>Security Settings (TLS) with maskSettings, socketSettings and finalRules</strong></summary>

```json
{
  "tlsSettings": {
    "alpn": ["h2", "http/1.1"],
    "certMode": "http",
	"certEmail": "author@cert.xyz",
    "certDomainName": "tld.dev",
	"dnsProvider": "cloudflare",
	"certFile": "/etc/XMRay/node3.crt",
    "keyFile": "/etc/XMRay/node3.key",
	"cipherSuites": "",
	"minVersion": "1.2",
	"maxVersion": "1.3",
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
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "AsIs",
    "tcpFastOpen": false,
    "tcpKeepAliveInterval": 0,
    "tcpKeepAliveIdle": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr",
    "v6only": false,
    "trustedXForwardedFor": []
  },
  "maskSettings": {
    "udp": [
      {
        "type": "salamander",
        "settings": {
          "password": "your-password-here",
          "packetSize": { "from": 512, "to": 1200 }
        }
      }
    ]
  },
  "finalRules": [
    {
      "action": "block",
      "network": "tcp,udp",
      "port": "53,443",
      "ip": ["10.0.0.0/8", "2001:db8::/32"],
      "blockDelay": "30-90"
    }
  ]
}
```
</details>

#### REALITY

```json
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


<details>
<summary><strong>Security Settings(Reality) with maskSettings, socketSettings and finalRules</strong></summary>

```json
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
  "socketSettings": {
    "acceptProxyProtocol": false,
    "domainStrategy": "AsIs",
    "tcpFastOpen": false,
    "tcpKeepAliveInterval": 0,
    "tcpKeepAliveIdle": 0,
    "tcpUserTimeout": 0,
    "tcpMaxSeg": 0,
    "tcpWindowClamp": 0,
    "tcpMptcp": false,
    "tcpCongestion": "bbr",
    "interface": "",
    "v6only": false,
    "dialerProxy": "",
    "trustedXForwardedFor": []
  },
  "maskSettings": {
    "udp": [
      {
        "type": "salamander",
        "settings": {
          "password": "your-password-here",
          "packetSize": { "from": 512, "to": 1200 }
        }
      }
    ]
  },
  "finalRules": [
    {
      "action": "block",
      "network": "tcp,udp",
      "port": "53,443",
      "ip": ["10.0.0.0/8", "2001:db8::/32"],
      "blockDelay": "30-90"
    }
  ]
}
```
</details>


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