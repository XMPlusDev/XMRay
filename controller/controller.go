package controller

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/xtls/xray-core/core"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/core/dispatcher"
	"github.com/xmplusdev/xmray/helper/cert"
	"github.com/xmplusdev/xmray/helper/task"
	"github.com/xmplusdev/xmray/node"
	"github.com/xmplusdev/xmray/subscription"
)

type TriggerInterface interface {
	TriggerNodeSync()
	TriggerSubscriptionSync()
	GetNodeID() int
}

type Controller struct {
	server           *core.Instance
	config           *node.Config
	dispatcher       *dispatcher.LimitingDispatcher
	clientInfo       api.ClientInfo
	client           api.API
	nodeInfo         *api.NodeInfo
	relaynodeInfo    *api.RelayNodeInfo
	Tag              string
	LogPrefix        string
	RelayTag         string
	Relay            bool
	subscriptionList *[]api.SubscriptionInfo
	taskManager      *task.Manager
	nodeManager      *node.Manager
	subManager       *subscription.Manager

	nodeSyncTrigger chan struct{}
	subscriptionSyncTrigger chan struct{}
	triggerCtx      context.Context
	triggerCancel   context.CancelFunc
}

// New returns a Controller service with default parameters.
func New(server *core.Instance, api api.API, config *node.Config, dispatcher *dispatcher.LimitingDispatcher) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{
		server:          server,
		config:          config,
		client:          api,
		taskManager:     task.NewManager(),
		nodeManager:     node.NewManager(server, dispatcher),
		subManager:      subscription.NewManager(server, api, dispatcher),
		nodeSyncTrigger: make(chan struct{}, 1),
		subscriptionSyncTrigger: make(chan struct{}, 1),
		triggerCtx:      ctx,
		triggerCancel:   cancel,
	}
}

func (c *Controller) TriggerNodeSync() {
	select {
	case c.nodeSyncTrigger <- struct{}{}:
	default: 
	}
}

func (c *Controller) TriggerSubscriptionSync() {
	select {
	case c.subscriptionSyncTrigger <- struct{}{}:
	default: 
	}
}

func (c *Controller) GetNodeID() int {
	return c.clientInfo.NodeID
}

// Start implements the Start() function of the service interface.
func (c *Controller) Start() error {
	c.clientInfo = c.client.Describe()

	newNodeInfo, err := c.client.GetNodeInfo()
	if err != nil {
		return err
	}
	c.nodeInfo = newNodeInfo
	c.Tag = c.buildNodeTag()

	// Fetch initial subscription list
	subscriptionInfo, err := c.client.GetSubscriptionList()
	if err != nil {
		return err
	}
	c.subscriptionList = subscriptionInfo

	c.Relay = false

	// Add relay tag if needed
	if c.nodeInfo.RelayType == 1 && c.nodeInfo.RelayNodeID > 0 {
		newRelayNodeInfo, err := c.client.GetTransitNode()
		if err != nil {
			log.Panic(err)
			return nil
		}
		c.relaynodeInfo = newRelayNodeInfo
		c.RelayTag = c.buildRNodeTag()

		err = c.nodeManager.AddRelayTag(
			newRelayNodeInfo,
			c.RelayTag,
			c.Tag,
			c.subscriptionList,
		)
		if err != nil {
			log.Panic(err)
			return err
		}
		c.Relay = true
	}

	err = c.nodeManager.AddRuleTag(c.nodeInfo, c.Tag)
	if err != nil {
		log.Panic(err)
		return err
	}

	err = c.nodeManager.AddTag(c.nodeInfo, c.Tag, c.config)
	if err != nil {
		log.Panic(err)
		return err
	}

	err = c.subManager.AddNewSubscription(subscriptionInfo, newNodeInfo, c.Tag)
	if err != nil {
		return err
	}
	log.Printf("%s Added %d subscriptions", c.logPrefix(), len(*subscriptionInfo))

	err = c.nodeManager.AddInboundLimiter(
		c.Tag,
		c.nodeInfo.UpdateTime,
		newNodeInfo.SpeedLimit,
		subscriptionInfo,
		c.config.RedisConfig,
	)
	if err != nil {
		log.Print(err)
	}

	c.LogPrefix = c.logPrefix()

	pollInterval := c.pollInterval()

	c.taskManager.Add(task.NewWithDelay(
		c.LogPrefix,
		"server",
		pollInterval,
		c.apiMonitor,
	))

	c.taskManager.Add(task.NewWithDelay(
		c.LogPrefix,
		"subscriptions",
		pollInterval,
		func() error {
			return c.subManager.SubscriptionMonitor(c.subscriptionList, c.Tag, c.LogPrefix)
		},
	))

	if c.nodeInfo.SecurityType == "tls" {
		if c.nodeInfo.TlsSettings.CertMode != "none" {
			c.taskManager.Add(task.NewWithDelay(
				c.LogPrefix,
				"cert renew",
				pollInterval*60,
				c.certMonitor,
			))
		}
	}

	go c.webhookTriggerLoop(pollInterval)

	log.Printf("%s Starting %d task schedulers", c.logPrefix(), c.taskManager.Count())
	return c.taskManager.StartAll()
}

// Close implements the Close() function of the service interface.
func (c *Controller) Close() error {
	log.Printf("%s Closing %d task schedulers", c.logPrefix(), c.taskManager.Count())

	c.triggerCancel()

	return c.taskManager.CloseAll()
}

func (c *Controller) webhookTriggerLoop(fallbackInterval time.Duration) {
	const debounceDuration = 3 * time.Second

	ticker := time.NewTicker(fallbackInterval)
	defer ticker.Stop()

	var lastSync time.Time

	for {
		select {

		case <-c.triggerCtx.Done():
			return

		case <-c.nodeSyncTrigger:
			if time.Since(lastSync) < debounceDuration {
				log.Printf("%s Webhook node trigger debounced", c.LogPrefix)
				c.drainChannel(c.nodeSyncTrigger)
				continue
			}
			log.Printf("%s Webhook node trigger: syncing now", c.LogPrefix)
			if err := c.apiMonitor(); err != nil {
				log.Printf("%s Webhook node sync error: %v", c.LogPrefix, err)
			}
			lastSync = time.Now()
			c.drainChannel(c.nodeSyncTrigger)
			ticker.Reset(fallbackInterval)

		case <-c.subscriptionSyncTrigger:
			if time.Since(lastSync) < debounceDuration {
				log.Printf("%s Webhook subscription trigger debounced", c.LogPrefix)
				c.drainChannel(c.subscriptionSyncTrigger)
				continue
			}
			log.Printf("%s Webhook subscription trigger: syncing now", c.LogPrefix)
			if err := c.apiMonitor(); err != nil {
				log.Printf("%s Webhook subscription sync error: %v", c.LogPrefix, err)
			}
			lastSync = time.Now()
			c.drainChannel(c.subscriptionSyncTrigger)
			ticker.Reset(fallbackInterval)

		case <-ticker.C:
			lastSync = time.Now()
		}
	}
}

func (c *Controller) drainChannel(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (c *Controller) pollInterval() time.Duration {
	return time.Duration(c.nodeInfo.UpdateTime) * time.Second
}

func (c *Controller) apiMonitor() (err error) {
	var nodeInfoChanged = true
	newNodeInfo, err := c.client.GetNodeInfo()
	if err != nil {
		if err.Error() == api.NodeNotModified {
			nodeInfoChanged = false
			newNodeInfo = c.nodeInfo
		} else {
			log.Print(err)
			return nil
		}
	}

	var subscriptionChanged = true
	newSubscriptionInfo, err := c.client.GetSubscriptionList()
	if err != nil {
		if err.Error() == api.SubscriptionNotModified {
			subscriptionChanged = false
			newSubscriptionInfo = c.subscriptionList
		} else {
			log.Print(err)
			return nil
		}
	}

	InfoUpdated := subscriptionChanged || nodeInfoChanged

	if c.Relay && InfoUpdated {
		if err := c.nodeManager.RemoveRelayRules(c.RelayTag, c.subscriptionList); err != nil {
			log.Print(err)
		}
		if err := c.nodeManager.RemoveRelayTag(c.RelayTag, c.subscriptionList); err != nil {
			return err
		}
		c.Relay = false
	}

	if newNodeInfo.RelayType == 1 && newNodeInfo.RelayNodeID > 0 && InfoUpdated {
		newRelayNodeInfo, err := c.client.GetTransitNode()
		if err != nil {
			log.Panic(err)
			return nil
		}
		c.relaynodeInfo = newRelayNodeInfo
		c.RelayTag = c.buildRNodeTag()

		err = c.nodeManager.AddRelayTag(
			newRelayNodeInfo,
			c.RelayTag,
			c.Tag,
			newSubscriptionInfo,
		)
		if err != nil {
			log.Panic(err)
			return err
		}
		c.Relay = true
	}

	if nodeInfoChanged {
		if !reflect.DeepEqual(c.nodeInfo, newNodeInfo) {
			oldTag := c.Tag
			if err := c.nodeManager.RemoveTag(oldTag); err != nil {
				log.Print(err)
				return nil
			}
			if err := c.nodeManager.RemoveBlockingRules(oldTag); err != nil {
				log.Print(err)
			}

			c.nodeInfo = newNodeInfo
			c.Tag = c.buildNodeTag()

			if err := c.nodeManager.AddRuleTag(newNodeInfo, c.Tag); err != nil {
				log.Print(err)
				return nil
			}
			if err := c.nodeManager.AddTag(newNodeInfo, c.Tag, c.config); err != nil {
				log.Print(err)
				return nil
			}
			if err := c.nodeManager.DeleteInboundLimiter(oldTag); err != nil {
				log.Print(err)
				return nil
			}
		} else {
			nodeInfoChanged = false
		}
	}

	if nodeInfoChanged {
		if err := c.subManager.AddNewSubscription(newSubscriptionInfo, newNodeInfo, c.Tag); err != nil {
			log.Print(err)
			return nil
		}
		if err := c.nodeManager.AddInboundLimiter(
			c.Tag,
			newNodeInfo.UpdateTime,
			newNodeInfo.SpeedLimit,
			newSubscriptionInfo,
			c.config.RedisConfig,
		); err != nil {
			log.Print(err)
			return nil
		}
	} else if subscriptionChanged {
		deleted, added, modified := subscription.Compare(c.subscriptionList, newSubscriptionInfo)

		if len(deleted) > 0 {
			deletedEmail := subscription.FormatEmails(deleted, c.Tag)
			if err := c.subManager.Remove(deletedEmail, c.Tag); err != nil {
				log.Printf("%s Error removing subscriptions: %v", c.LogPrefix, err)
			} else {
				log.Printf("%s Removed %d subscription(s)", c.LogPrefix, len(deleted))
				c.nodeManager.DeleteSubscriptionBuckets(c.Tag, deletedEmail)
			}
		}

		if len(added) > 0 {
			if err := c.subManager.AddNewSubscription(&added, c.nodeInfo, c.Tag); err != nil {
				log.Printf("%s Error adding subscriptions: %v", c.LogPrefix, err)
			} else {
				log.Printf("%s Added %d subscription(s)", c.LogPrefix, len(added))
				if err := c.nodeManager.UpdateInboundLimiter(c.Tag, &added); err != nil {
					log.Printf("%s Error updating limiter for new subscriptions: %v", c.LogPrefix, err)
				}
			}
		}

		if len(modified) > 0 {
			deletedEmail := subscription.FormatEmails(modified, c.Tag)
			if err := c.subManager.Remove(deletedEmail, c.Tag); err != nil {
				log.Printf("%s Error removing modified subscriptions: %v", c.LogPrefix, err)
			} else {
				c.nodeManager.DeleteSubscriptionBuckets(c.Tag, deletedEmail)
			}
			if err := c.subManager.AddNewSubscription(&modified, c.nodeInfo, c.Tag); err != nil {
				log.Printf("%s Error re-adding modified subscriptions: %v", c.LogPrefix, err)
			}
			if err := c.nodeManager.UpdateInboundLimiter(c.Tag, &modified); err != nil {
				log.Printf("%s Error updating limiter for modified subscriptions: %v", c.LogPrefix, err)
			}
			log.Printf("%s Modified %d subscription(s)", c.LogPrefix, len(modified))
		}
	}

	c.subscriptionList = newSubscriptionInfo
	return nil
}

func (c *Controller) certMonitor() error {
	switch c.nodeInfo.TlsSettings.CertMode {
	case "dns", "http", "tls":
		lego, err := cert.New(c.config.CertConfig)
		if err != nil {
			log.Print(err)
		}
		_, _, _, err = lego.RenewCert(
			c.nodeInfo.TlsSettings.CertMode,
			c.nodeInfo.TlsSettings.CertDomainName,
		)
		if err != nil {
			log.Print(err)
		}
	}
	return nil
}

func (c *Controller) logPrefix() string {
	return fmt.Sprintf("[%s] %s(NodeID=%d)",
		c.clientInfo.APIHost,
		c.nodeInfo.NodeType,
		c.nodeInfo.NodeID)
}

func (c *Controller) buildNodeTag() string {
	return fmt.Sprintf("%s_%s_%d",
		c.nodeInfo.NodeType,
		c.nodeInfo.ListeningPort,
		c.nodeInfo.NodeID)
}

func (c *Controller) buildRNodeTag() string {
	return fmt.Sprintf("Relay_%s_%d_%d",
		c.relaynodeInfo.NodeType,
		c.relaynodeInfo.ListeningPort,
		c.relaynodeInfo.NodeID)
}