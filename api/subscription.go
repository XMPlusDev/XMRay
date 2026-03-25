package api

import (
	"encoding/json"
	"fmt"
	"errors"
	"reflect"
	"strconv"
	
	"github.com/go-resty/resty/v2"
)

func (c *Client) parseSubscriptionResponse(res *resty.Response, err error) (*SubscriptionResponse, error) {
	if err != nil {
		return nil, fmt.Errorf("Failed to subscription lists request: %s", err)
	}

	if res.StatusCode() >= 400 {
		return nil, fmt.Errorf("Subscription request error: %v", err)
	}
	
	response := res.Result().(*SubscriptionResponse)

	return response, nil
}

func (c *Client) GetSubscriptionList() (SubscriptionList *[]SubscriptionInfo, err error) {
	
	res, err := c.client.R().
		SetBody(map[string]string{"key": c.Key}).
		SetHeader("If-None-Match", c.eTags["subscriptions"]).
		SetPathParam("serverId", strconv.Itoa(c.NodeID)).
		SetResult(&SubscriptionResponse{}).
		ForceContentType("application/json").
		Post("/api/server/subscription/lists/{serverId}")
	
	if res.StatusCode() == 304 {
		return nil, errors.New(SubscriptionNotModified)
	}

	if res.Header().Get("Etag") != "" && res.Header().Get("Etag") != c.eTags["subscriptions"] {
		c.eTags["subscriptions"] = res.Header().Get("Etag")
	}

	response, err := c.parseSubscriptionResponse(res, err)
	if err != nil {
		return nil, err
	}
	
	subscriptionsListResponse := new([]Subscription)

	if err := json.Unmarshal(response.Data, subscriptionsListResponse); err != nil {
		return nil, fmt.Errorf("unmarshal %s failed: %s", reflect.TypeOf(subscriptionsListResponse), err)
	}
	
	subscriptionList, err := c.ParseSubscriptionList(subscriptionsListResponse)
	if err != nil {
		res, _ := json.Marshal(subscriptionsListResponse)
		return nil, fmt.Errorf("parse subscription list failed: %s", string(res))
	}
	
	return subscriptionList, nil
}

func (c *Client) ParseSubscriptionList(subscriptionResponse *[]Subscription) (*[]SubscriptionInfo, error) {
	subscriptionList := make([]SubscriptionInfo, 0, len(*subscriptionResponse))
	
	for _, subscription := range *subscriptionResponse {
		speedLimit := uint64((subscription.Speedlimit * 1000000) / 8)
		
		subscriptionList = append(subscriptionList, SubscriptionInfo{
			Id:         subscription.Id,
			Email:      subscription.Email,
			Passwd:     subscription.Passwd,
			IPLimit:    subscription.Iplimit,
			SpeedLimit: speedLimit,
		})
	}

	return &subscriptionList, nil
}

func (c *Client) ReportTraffic(subscriptionTraffic *[]SubscriptionTraffic) error {
	data := make([]Traffic, len(*subscriptionTraffic))	
	for i, traffic := range *subscriptionTraffic {
		data[i] = Traffic{
			Id:  traffic.Id,
			Upload:   traffic.Upload,
			Download:   traffic.Download,
		}
	}
	
	postData := &PostData{
		Key:  c.Key,
		Data: data,
	}
	res, err := c.client.R().
		SetBody(postData).
		SetPathParam("serverId", strconv.Itoa(c.NodeID)).
		ForceContentType("application/json").
		Post("/api/server/subscription/traffic/{serverId}")
		
	_, err = c.checkResponse(res, err)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) ReportOnlineIPs(onlineSubscriptionList *[]OnlineIP) error {
	c.access.Lock()
	defer c.access.Unlock()

	reportOnline := make(map[int]int)
	
	data := make([]AliveIP, len(*onlineSubscriptionList))
	for i, subscription := range *onlineSubscriptionList {
		data[i] = AliveIP{
			Id: subscription.Id, 
			IP: subscription.IP,
		}
		if _, ok := reportOnline[subscription.Id]; ok {
			reportOnline[subscription.Id]++
		} else {
			reportOnline[subscription.Id] = 1
		}
	}
	c.LastReportOnline = reportOnline 

	postData := &PostData{
		Key:  c.Key,
		Data: data,
	}
	
	fmt.Errorf("onlineips %v", postData)
	
	res, err := c.client.R().
		SetBody(postData).
		SetPathParam("serverId", strconv.Itoa(c.NodeID)).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Post("/api/server/subscription/onlineip/{serverId}")

	_, err = c.checkResponse(res, err)
	if err != nil {
		return err
	}

	return nil
}