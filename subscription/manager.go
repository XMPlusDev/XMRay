package subscription

import (
	"context"
	"fmt"
	"log"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/core/dispatcher"
	"github.com/xmplusdev/xmray/limiter"

	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/proxy"
)

type Manager struct {
	server     *core.Instance
	client     api.API
	ibm        inbound.Manager
	dispatcher *dispatcher.LimitingDispatcher
}

func NewManager(server *core.Instance, client api.API, d *dispatcher.LimitingDispatcher) *Manager {
	return &Manager{
		server:     server,
		client:     client,
		ibm:        server.GetFeature(inbound.ManagerType()).(inbound.Manager),
		dispatcher: d,
	}
}

func (m *Manager) AddNewSubscription(subscriptionInfo *[]api.SubscriptionInfo, nodeInfo *api.NodeInfo, tag string) error {
	if subscriptionInfo == nil || len(*subscriptionInfo) == 0 {
		return nil
	}

	var users []*protocol.User
	switch nodeInfo.NodeType {
	case "vless":
		users = BuildVlessUsers(subscriptionInfo, nodeInfo.Flow, tag)
	case "vmess":
		users = BuildVmessUsers(subscriptionInfo, tag)
	case "trojan":
		users = BuildTrojanUsers(subscriptionInfo, tag)
	case "shadowsocks":
		users = BuildShadowsocksUsers(subscriptionInfo, nodeInfo.Cipher, tag)
	case "hysteria":
		users = BuildHysteriaUsers(subscriptionInfo, tag)
	default:
		return fmt.Errorf("unsupported node type %s. Abort building user", nodeInfo.NodeType)
	}

	return m.Add(users, tag)
}

func (m *Manager) Add(subscriptions []*protocol.User, tag string) error {
	if len(subscriptions) == 0 {
		return nil
	}
	return m.addInboundSubscriptions(subscriptions, tag)
}

func (m *Manager) Remove(emails []string, tag string) error {
	if len(emails) == 0 {
		return nil
	}
	if err := m.removeInboundSubscriptions(emails, tag); err != nil {
		return fmt.Errorf("failed to remove subscriptions from tag %s: %w", tag, err)
	}
	return nil
}

// Compare compares two subscription lists.
// deleted: IDs in old but not in new; added: IDs in new but not in old; modified: IDs in both but changed.
func Compare(old, new *[]api.SubscriptionInfo) (deleted, added, modified []api.SubscriptionInfo) {
	if old == nil && new == nil {
		return nil, nil, nil
	}
	if old == nil {
		return nil, *new, nil
	}
	if new == nil {
		return *old, nil, nil
	}

	oldMap := make(map[int]api.SubscriptionInfo)
	newMap := make(map[int]api.SubscriptionInfo)
	for _, v := range *old {
		oldMap[v.Id] = v
	}
	for _, v := range *new {
		newMap[v.Id] = v
	}

	for id, oldSub := range oldMap {
		if _, exists := newMap[id]; !exists {
			deleted = append(deleted, oldSub)
		}
	}
	for id, newSub := range newMap {
		if oldSub, exists := oldMap[id]; !exists {
			added = append(added, newSub)
		} else if oldSub.SpeedLimit != newSub.SpeedLimit ||
			oldSub.IPLimit != newSub.IPLimit ||
			oldSub.Passwd != newSub.Passwd ||
			oldSub.Email != newSub.Email {
			modified = append(modified, newSub)
		}
	}
	return
}

const reverbBatchSize = 100

func (m *Manager) reportTraffic(pending *limiter.PendingTraffic, logPrefix string, pusher func(string, any) error) {
	var undelivered []*limiter.PendingTraffic
	pushed := 0

	for _, chunk := range pending.Chunk(reverbBatchSize) {
		if pusher == nil {
			undelivered = append(undelivered, chunk)
			continue
		}
		traffic := make([]api.Traffic, len(chunk.Result))
		for idx, t := range chunk.Result {
			traffic[idx] = api.Traffic{Id: t.Id, Upload: t.Upload, Download: t.Download}
		}
		if err := pusher("traffic_report", traffic); err != nil {
			log.Printf("%s Failed to push traffic data via Reverb: %v", logPrefix, err)
			undelivered = append(undelivered, chunk)
			continue
		}
		m.dispatcher.ResetTraffic(chunk)
		pushed += len(chunk.Result)
	}
	if pushed > 0 {
		log.Printf("%s Pushed %d Traffic Usage Data via Reverb", logPrefix, pushed)
	}
	if len(undelivered) == 0 {
		return
	}

	records := make([]api.SubscriptionTraffic, 0, len(undelivered)*reverbBatchSize)
	for _, chunk := range undelivered {
		records = append(records, chunk.Result...)
	}
	if err := m.client.ReportTraffic(&records); err != nil {
		log.Printf("%s Failed to report traffic data: %v", logPrefix, err)
		return
	}
	log.Printf("%s Report %d Traffic Usage Data", logPrefix, len(records))
	for _, chunk := range undelivered {
		m.dispatcher.ResetTraffic(chunk)
	}
}

func (m *Manager) reportOnlineIPs(onlineIPs []api.OnlineIP, logPrefix string, pusher func(string, any) error) {
	var undelivered []api.OnlineIP
	pushed := 0

	for start := 0; start < len(onlineIPs); start += reverbBatchSize {
		batch := onlineIPs[start:min(start+reverbBatchSize, len(onlineIPs))]
		if pusher == nil {
			undelivered = append(undelivered, batch...)
			continue
		}
		aliveIPs := make([]api.AliveIP, len(batch))
		for idx, ip := range batch {
			aliveIPs[idx] = api.AliveIP{Id: ip.Id, IP: ip.IP}
		}
		if err := pusher("online_ips", aliveIPs); err != nil {
			log.Printf("%s Failed to push online IPs via Reverb: %v", logPrefix, err)
			undelivered = append(undelivered, batch...)
			continue
		}
		pushed += len(batch)
	}
	if pushed > 0 {
		log.Printf("%s Pushed %d Online IPs Data via Reverb", logPrefix, pushed)
	}
	if len(undelivered) == 0 {
		return
	}

	if err := m.client.ReportOnlineIPs(&undelivered); err != nil {
		log.Printf("%s Failed to report online IPs: %v", logPrefix, err)
		return
	}
	log.Printf("%s Report %d Online IPs Data", logPrefix, len(undelivered))
}

func (m *Manager) SubscriptionMonitor(tag string, logPrefix string, pusher func(string, any) error) error {
	if pendingTraffic := m.dispatcher.DrainDeltas(tag); pendingTraffic != nil {
		m.reportTraffic(pendingTraffic, logPrefix, pusher)
	}

	onlineIPs, err := m.GetOnlineIPs(tag)
	if err != nil {
		log.Print(err)
	} else if onlineIPs != nil && len(*onlineIPs) > 0 {
		m.reportOnlineIPs(*onlineIPs, logPrefix, pusher)
	}
	return nil
}

func FormatEmails(subscriptions []api.SubscriptionInfo, tag string) []string {
	if len(subscriptions) == 0 {
		return nil
	}
	emails := make([]string, len(subscriptions))
	for i, u := range subscriptions {
		emails[i] = fmt.Sprintf("%s_%s", tag, u.Email)
	}
	return emails
}

func buildUserTag(tag string, subscription *api.SubscriptionInfo) string {
	return fmt.Sprintf("%s_%s", tag, subscription.Email)
}

func (m *Manager) GetOnlineIPs(tag string) (*[]api.OnlineIP, error) {
	return m.dispatcher.GetOnlineIPs(tag)
}

func (m *Manager) addInboundSubscriptions(subscriptions []*protocol.User, tag string) error {
	handler, err := m.ibm.GetHandler(context.Background(), tag)
	if err != nil {
		return fmt.Errorf("no such inbound tag: %s", err)
	}
	inboundInstance, ok := handler.(proxy.GetInbound)
	if !ok {
		return fmt.Errorf("handler %s has not implemented proxy.GetInbound", tag)
	}
	userManager, ok := inboundInstance.GetInbound().(proxy.UserManager)
	if !ok {
		return fmt.Errorf("handler %s has not implemented proxy.UserManager", tag)
	}
	for _, item := range subscriptions {
		subscription, err := item.ToMemoryUser()
		if err != nil {
			return err
		}
		if err = userManager.AddUser(context.Background(), subscription); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) removeInboundSubscriptions(emails []string, tag string) error {
	handler, err := m.ibm.GetHandler(context.Background(), tag)
	if err != nil {
		return fmt.Errorf("no such inbound tag: %s", err)
	}
	inboundInstance, ok := handler.(proxy.GetInbound)
	if !ok {
		return fmt.Errorf("handler %s is not implement proxy.GetInbound", tag)
	}
	userManager, ok := inboundInstance.GetInbound().(proxy.UserManager)
	if !ok {
		return fmt.Errorf("handler %s is not implement proxy.UserManager", tag)
	}
	for _, email := range emails {
		if err = userManager.RemoveUser(context.Background(), email); err != nil {
			return err
		}
	}
	return nil
}
