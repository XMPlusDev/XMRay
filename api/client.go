package api

import (
	"fmt"
	"log"
	"strings"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitly/go-simplejson"
	"github.com/go-resty/resty/v2"
	"github.com/xtls/xray-core/infra/conf"
)

type Client struct {
	client           *resty.Client
	APIHost          string
	NodeID           int
	ServerID         int
	Key              string
	resp             atomic.Value
	eTags            map[string]string
	LastReportOnline map[int]int
	access           sync.Mutex
}

type ClientInfo struct {
	APIHost string
	NodeID  int
	ServerID  int
	Key     string
}

func New(apiConfig *Config) *Client {
	if !strings.HasPrefix(apiConfig.APIHost, "https://") {
		log.Fatalf("ERROR: API host must use HTTPS protocol. Got: %s", apiConfig.APIHost)
	}

	client := resty.New()
	client.SetRetryCount(5)
	if apiConfig.Timeout > 0 {
		client.SetTimeout(time.Duration(apiConfig.Timeout) * time.Second)
	} else {
		client.SetTimeout(30 * time.Second)
	}

	client.OnError(func(req *resty.Request, err error) {
		if v, ok := err.(*resty.ResponseError); ok {
			log.Print(v.Err)
		}
	})

	client.SetBaseURL(apiConfig.APIHost)

	return &Client{
		client:           client,
		NodeID:           apiConfig.NodeID,
		ServerID:         apiConfig.ServerID,
		Key:              apiConfig.Key,
		APIHost:          apiConfig.APIHost,
		LastReportOnline: make(map[int]int),
		eTags:            make(map[string]string),
	}
}

func (c *Client) ForNode(nodeID int) *Client {
	return &Client{
		client:           c.client,
		APIHost:          c.APIHost,
		NodeID:           nodeID,
		ServerID:         c.ServerID,
		Key:              c.Key,
		LastReportOnline: make(map[int]int),
		eTags:            make(map[string]string),
	}
}

func (c *Client) Describe() ClientInfo {
	return ClientInfo{
		APIHost: c.APIHost,
		NodeID:  c.NodeID,
		ServerID: c.ServerID,
		Key:     c.Key,
	}
}

func (c *Client) Debug() {
	c.client.SetDebug(true)
}

func (c *Client) checkResponse(res *resty.Response, err error) (*simplejson.Json, error) {
	if err != nil {
		var requestURL string
		if res != nil && res.Request != nil && res.Request.RawRequest != nil {
			requestURL = res.Request.RawRequest.URL.String()
		}
		return nil, fmt.Errorf("request error for URL %s: %s", requestURL, err)
	}

	if res.StatusCode() >= 400 {
		requestURL := "unknown"
		if res.Request != nil && res.Request.RawRequest != nil {
			requestURL = res.Request.RawRequest.URL.String()
		}
		return nil, fmt.Errorf("request %s failed: %s", requestURL, string(res.Body()))
	}

	result, err := simplejson.NewJson(res.Body())
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %s", res.String())
	}

	return result, nil
}

// ParsePorts turns ["53", "443", "1000-2000"] into port ranges.
// An entry may also hold a comma list ("53,443"), which older panel rows used.
func ParsePorts(ports []string) ([]conf.PortRange, error) {
	parse := func(s string) (uint32, error) {
		n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 16) // 16 bits: max 65535
		if err != nil || n == 0 {
			return 0, fmt.Errorf("invalid port number: %s", s)
		}
		return uint32(n), nil
	}

	var ranges []conf.PortRange
	for _, entry := range ports {
		for _, seg := range strings.Split(entry, ",") {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			if i := strings.Index(seg, "-"); i >= 0 {
				from, err := parse(seg[:i])
				if err != nil {
					return nil, err
				}
				to, err := parse(seg[i+1:])
				if err != nil {
					return nil, err
				}
				if from > to {
					return nil, fmt.Errorf("start port %d > end port %d", from, to)
				}
				ranges = append(ranges, conf.PortRange{From: from, To: to})
				continue
			}
			p, err := parse(seg)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, conf.PortRange{From: p, To: p})
		}
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("port list is empty")
	}
	return ranges, nil
}