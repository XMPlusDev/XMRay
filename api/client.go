package api

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitly/go-simplejson"
	"github.com/go-resty/resty/v2"
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
