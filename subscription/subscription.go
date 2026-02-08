package subscription

import (
	"context"
	"fmt"
	"log"

	"github.com/xmplusdev/xmplus-server/api"
	"github.com/xmplusdev/xmplus-server/app/dispatcher"
	
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/features/routing"
)

// Manager handles subscription-related operations
type Manager struct {
	server *core.Instance
	client  api.API  
	ibm    inbound.Manager
	stm    stats.Manager
	dispatcher   *dispatcher.DefaultDispatcher
}

// NewManager creates a new subscription manager
func NewManager(server *core.Instance, client api.API) *Manager {
	return &Manager{
		server: server,
		client: client,
		ibm:    server.GetFeature(inbound.ManagerType()).(inbound.Manager),
		stm:    server.GetFeature(stats.ManagerType()).(stats.Manager),
		dispatcher:  server.GetFeature(routing.DispatcherType()).(*dispatcher.DefaultDispatcher),
	}
}

func (m *Manager) AddNewSubscription(subscriptionInfo *[]api.SubscriptionInfo, nodeInfo *api.NodeInfo, tag string) (err error) {
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
	default:
		return fmt.Errorf("unsupported node type %s. Abort building user", nodeInfo.NodeType)
	}

	return m.Add(users, tag)
}

// Add adds new subscriptions to an inbound tag
func (m *Manager) Add(subscriptions []*protocol.User, tag string) error {
	if len(subscriptions) == 0 {
		return nil
	}

	err := m.addInboundSubscriptions(subscriptions, tag)
	if err != nil {
		return fmt.Errorf("failed to add subscriptions to tag %s: %w", tag, err)
	}

	log.Printf("Added %d subscriptions to tag %s", len(subscriptions), tag)
	return nil
}

// Remove removes subscriptions from an inbound tag
func (m *Manager) Remove(emails []string, tag string) error {
	if len(emails) == 0 {
		return nil
	}

	err := m.removeInboundSubscriptions(emails, tag)
	if err != nil {
		return fmt.Errorf("failed to remove subscriptions from tag %s: %w", tag, err)
	}

	log.Printf("Removed %d subscriptions from tag %s", len(emails), tag)
	return nil
}

// Compare compares two subscription lists based on ID only
// deleted: subscriptions whose IDs are in old but not in new
// added: subscriptions whose IDs are in new but not in old  
// modified: subscriptions whose IDs exist in both but properties changed
func Compare(old, new *[]api.SubscriptionInfo) (deleted, added, modified []api.SubscriptionInfo) {
	// Handle nil cases
	if old == nil && new == nil {
		return nil, nil, nil
	}
	if old == nil {
		return nil, *new, nil
	}
	if new == nil {
		return *old, nil, nil
	}

	// Create maps using Id as the key
	oldMap := make(map[int]api.SubscriptionInfo)
	newMap := make(map[int]api.SubscriptionInfo)

	// Populate oldMap
	for _, v := range *old {
		oldMap[v.Id] = v
	}

	// Populate newMap
	for _, v := range *new {
		newMap[v.Id] = v
	}

	// Find deleted subscriptions (ID in old but not in new)
	for id, oldSub := range oldMap {
		if _, exists := newMap[id]; !exists {
			deleted = append(deleted, oldSub)
		}
	}

	// Find added and modified subscriptions
	for id, newSub := range newMap {
		if oldSub, exists := oldMap[id]; !exists {
			// ID doesn't exist in old - it's new
			added = append(added, newSub)
		} else {
			// ID exists in both - check if properties changed
			if oldSub.SpeedLimit != newSub.SpeedLimit || 
			   oldSub.IPLimit != newSub.IPLimit ||
			   oldSub.Passwd != newSub.Passwd ||
			   oldSub.Email != newSub.Email {
				modified = append(modified, newSub)
			}
		}
	}

	return deleted, added, modified
}


func (m *Manager) SubscriptionMonitor(
	subscriptionList *[]api.SubscriptionInfo,
	tag string,
	logPrefix string,
) (err error) {  // Added closing parenthesis here
	// Get Subscription traffic
	var subscriptionTraffic []api.SubscriptionTraffic
	var upCounterList []stats.Counter
	var downCounterList []stats.Counter

	for _, subscription := range *subscriptionList {
		up, down, upCounter, downCounter := m.getTraffic(buildUserTag(tag, &subscription))
		if up > 0 || down > 0 {
			subscriptionTraffic = append(subscriptionTraffic, api.SubscriptionTraffic{
				Id: subscription.Id,
				Upload:  up,
				Download:  down,
			})  // Added closing brace and parenthesis here

			if upCounter != nil {
				upCounterList = append(upCounterList, upCounter)
			}
			if downCounter != nil {
				downCounterList = append(downCounterList, downCounter)
			}
		}
	}

	if len(subscriptionTraffic) > 0 {
		var err error // Define an empty error

		err = m.client.ReportTraffic(&subscriptionTraffic)
		// If report traffic error, not clear the traffic
		if err != nil {
			log.Print(err)
		} else {
			log.Printf("%s Report %d Subscription Traffic Usage Data", logPrefix, len(subscriptionTraffic))
			m.resetTraffic(&upCounterList, &downCounterList)
		}
	}

	// Report Online info
	onlineIPs, err := m.GetOnlineIPs(tag)
	if err != nil {
		log.Print(err)
	} else if len(*onlineIPs) > 0 {
		if err = m.client.ReportOnlineIPs(onlineIPs); err != nil {
			log.Print(err)
		} else {
			log.Printf("%s Report %d Subscription Online IPs Data", logPrefix, len(*onlineIPs))
		}
	}

	return nil
}

// FormatEmails formats subscription info into email strings for removal
func FormatEmails(subscriptions []api.SubscriptionInfo, tag string) []string {
	if len(subscriptions) == 0 {
		return nil
	}

	emails := make([]string, len(subscriptions))
	for i, u := range subscriptions {
		emails[i] = fmt.Sprintf("%s|%s|%d", tag, u.Email, u.Id)
	}
	return emails
}

func buildUserTag(tag string, subscription *api.SubscriptionInfo) string {
	return fmt.Sprintf("%s|%s|%d", tag, subscription.Email, subscription.Id)
}

// Private helper methods
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
		err = userManager.AddUser(context.Background(), subscription)
		if err != nil {
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
		return fmt.Errorf("handler %s is not implement proxy.UserManager", err)
	}
	for _, email := range emails {
		err = userManager.RemoveUser(context.Background(), email)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) getTraffic(email string) (up int64, down int64, upCounter stats.Counter, downCounter stats.Counter) {
	upName := "user>>>" + email + ">>>traffic>>>uplink"
	downName := "user>>>" + email + ">>>traffic>>>downlink"
	upCounter = m.stm.GetCounter(upName)
	downCounter = m.stm.GetCounter(downName)
	if upCounter != nil && upCounter.Value() != 0 {
		up = upCounter.Value()
	} else {
		upCounter = nil
	}
	if downCounter != nil && downCounter.Value() != 0 {
		down = downCounter.Value()
	} else {
		downCounter = nil
	}
	return up, down, upCounter, downCounter
}

func (m *Manager) resetTraffic(upCounterList *[]stats.Counter, downCounterList *[]stats.Counter) {
	for _, upCounter := range *upCounterList {
		upCounter.Set(0)
	}
	for _, downCounter := range *downCounterList {
		downCounter.Set(0)
	}
}

func (m *Manager) GetOnlineIPs(tag string) (*[]api.OnlineIP, error) {
	return m.dispatcher.Limiter.GetOnlineIPs(tag)
}