package controller

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/xtls/xray-core/core"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/helper/cert"
	"github.com/xmplusdev/xmray/core/dispatcher"
	"github.com/xmplusdev/xmray/helper/monitor"
	"github.com/xmplusdev/xmray/node"
	"github.com/xmplusdev/xmray/helper/scheduler"
	"github.com/xmplusdev/xmray/subscription"
)

const serverStatusReportInterval = 5 * time.Second

type Controller struct {
	server     *core.Instance
	config     *node.Config
	dispatcher *dispatcher.LimitingDispatcher
	clientInfo api.ClientInfo
	client     api.API
	nodeInfo   *api.NodeInfo
	pusher     func(event string, data any) error

	relaynodeInfo *api.RelayNodeInfo
	Tag           string
	LogPrefix     string
	RelayTag      string
	Relay         bool

	currentPollInterval time.Duration
	subscriptionList    *[]api.SubscriptionInfo
	taskManager         *scheduler.Manager
	nodeManager         *node.Manager
	subManager          *subscription.Manager

	nodeSyncTrigger         chan struct{}
	subscriptionSyncTrigger chan struct{}
	intervalChangeCh        chan time.Duration
	triggerCtx              context.Context
	triggerCancel           context.CancelFunc
}

func New(server *core.Instance, api api.API, config *node.Config, d *dispatcher.LimitingDispatcher, pusher func(string, any) error) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{
		server:                  server,
		config:                  config,
		client:                  api,
		dispatcher:              d,
		pusher:                  pusher,
		taskManager:             scheduler.NewManager(),
		nodeManager:             node.NewManager(server, d),
		subManager:              subscription.NewManager(server, api, d),
		nodeSyncTrigger:         make(chan struct{}, 1),
		subscriptionSyncTrigger: make(chan struct{}, 1),
		intervalChangeCh:        make(chan time.Duration, 1),
		triggerCtx:              ctx,
		triggerCancel:           cancel,
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

func (c *Controller) Start() error {
	c.clientInfo = c.client.Describe()

	newNodeInfo, err := c.client.GetNodeInfo()
	if err != nil {
		return err
	}
	c.nodeInfo = newNodeInfo
	c.Tag = c.buildNodeTag()

	subscriptionInfo, err := c.client.GetSubscriptionList()
	if err != nil {
		return err
	}
	c.subscriptionList = subscriptionInfo

	c.Relay = false

	if c.nodeInfo.RelayType == 1 && c.nodeInfo.RelayNodeID > 0 {
		newRelayNodeInfo, err := c.client.GetTransitNode()
		if err != nil {
			log.Panic(err)
			return nil
		}
		c.relaynodeInfo = newRelayNodeInfo
		c.RelayTag = c.buildRNodeTag()

		if err = c.nodeManager.AddRelayTag(newRelayNodeInfo, c.RelayTag, c.Tag, c.subscriptionList); err != nil {
			log.Panic(err)
			return err
		}
		c.Relay = true
	}

	if err = c.nodeManager.AddBlackHoleRuleTag(c.nodeInfo, c.Tag); err != nil {
		log.Panic(err)
		return err
	}

	if err = c.nodeManager.AddTag(c.nodeInfo, c.Tag, c.config); err != nil {
		log.Panic(err)
		return err
	}

	if err = c.subManager.AddNewSubscription(subscriptionInfo, newNodeInfo, c.Tag); err != nil {
		return err
	}
	log.Printf("%s Added %d subscriptions", c.logPrefix(), len(*subscriptionInfo))

	if err = c.nodeManager.AddInboundLimiter(
		c.Tag,
		c.nodeInfo.UpdateTime,
		newNodeInfo.SpeedLimit,
		newNodeInfo.IgnoreIPs,
		subscriptionInfo,
	); err != nil {
		log.Print(err)
	}

	c.LogPrefix = c.logPrefix()
	c.currentPollInterval = c.pollInterval()

	nodePusher := c.nodePusher()

	c.taskManager.Add(scheduler.NewWithDelay(c.LogPrefix, "node", c.currentPollInterval, c.apiMonitor))
	c.taskManager.Add(scheduler.NewWithDelay(c.LogPrefix, "subscriptions", c.currentPollInterval, func() error {
		return c.subManager.SubscriptionMonitor(c.Tag, c.LogPrefix, nodePusher)
	}))
	if !c.config.DisableServerMonitor {
		err := c.serverMonitor()
		if err != nil {
			log.Printf("%v", err)
		}

		c.taskManager.Add(scheduler.NewWithDelay(c.LogPrefix, "server_status", serverStatusReportInterval, c.serverMonitor))
	}

	if c.nodeInfo.SecurityType == "tls" && c.nodeInfo.TlsSettings != nil && c.nodeInfo.TlsSettings.CertMode != "none" {
		c.taskManager.Add(scheduler.NewWithDelay(c.LogPrefix, "cert_renew", c.currentPollInterval*60, c.certMonitor))
	}

	go c.webhookTriggerLoop(c.currentPollInterval)

	log.Printf("%s Starting %d task schedulers", c.logPrefix(), c.taskManager.Count())
	return c.taskManager.StartAll()
}

func (c *Controller) Close() error {
	log.Printf("%s Closing %d task schedulers", c.logPrefix(), c.taskManager.Count())
	c.triggerCancel()
	c.nodeManager.DeleteInboundLimiter(c.Tag)
	c.nodeManager.RemoveBlockingRules(c.Tag)
	if err := c.nodeManager.RemoveTag(c.Tag); err != nil {
		log.Printf("%s Close RemoveTag: %v", c.logPrefix(), err)
	}
	if c.Relay {
		if err := c.nodeManager.RemoveRelayRules(c.RelayTag, c.subscriptionList); err != nil {
			log.Printf("%s Close RemoveRelayRules: %v", c.logPrefix(), err)
		}
		if err := c.nodeManager.RemoveRelayTag(c.RelayTag, c.subscriptionList); err != nil {
			log.Printf("%s Close RemoveRelayTag: %v", c.logPrefix(), err)
		}
	}
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

		case newInterval := <-c.intervalChangeCh:
			ticker.Reset(newInterval)
			fallbackInterval = newInterval
			log.Printf("%s Webhook interval updated to %v", c.LogPrefix, newInterval)

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

func nodeInfoChangedStructurally(old, new *api.NodeInfo) bool {
	if old == nil || new == nil {
		return true
	}
	o, n := *old, *new
	o.SpeedLimit, n.SpeedLimit = 0, 0
	o.IgnoreIPs, n.IgnoreIPs = nil, nil
	o.UpdateTime, n.UpdateTime = 0, 0
	return !reflect.DeepEqual(o, n)
}

func (c *Controller) apiMonitor() (err error) {
	nodeInfoChanged := true
	newNodeInfo, err := c.client.GetNodeInfo()
	if err != nil {
		if err.Error() == api.NodeNotModified {
			nodeInfoChanged = false
			newNodeInfo = c.nodeInfo
		} else {
			log.Printf("%s Controller APIMonitor GetNodeInfo: %v", c.LogPrefix, err)
			return fmt.Errorf("%s Controller APIMonitor GetNodeInfo: %v", c.LogPrefix, err)
		}
	}

	subscriptionChanged := true
	newSubscriptionInfo, err := c.client.GetSubscriptionList()
	if err != nil {
		if err.Error() == api.SubscriptionNotModified {
			subscriptionChanged = false
			newSubscriptionInfo = c.subscriptionList
		} else {
			log.Printf("%s Controller APIMonitor GetSubscriptionList: %v", c.LogPrefix, err)
			return fmt.Errorf("%s Controller APIMonitor GetSubscriptionList: %v", c.LogPrefix, err)
		}
	}

	infoUpdated := subscriptionChanged || nodeInfoChanged

	if c.Relay && infoUpdated {
		if err := c.nodeManager.RemoveRelayRules(c.RelayTag, c.subscriptionList); err != nil {
			log.Printf("%s Controller APIMonitor RemoveRelayRules: %v", c.LogPrefix, err)
			return fmt.Errorf("Controller APIMonitor RemoveRelayRules: %w", err)
		}
		if err := c.nodeManager.RemoveRelayTag(c.RelayTag, c.subscriptionList); err != nil {
			log.Printf("%s Controller APIMonitor RemoveRelayTag: %v", c.LogPrefix, err)
			return fmt.Errorf("Controller APIMonitor RemoveRelayTag: %w", err)
		}
		c.Relay = false
	}

	if nodeInfoChanged && nodeInfoChangedStructurally(c.nodeInfo, newNodeInfo) {
		oldTag := c.Tag
		if err := c.nodeManager.RemoveBlockingRules(oldTag); err != nil {
			log.Printf("%s Controller APIMonitor RemoveBlockingRules: %v", c.LogPrefix, err)
		}
		if err := c.nodeManager.RemoveTag(oldTag); err != nil {
			log.Printf("%s Controller APIMonitor RemoveInboundTag: %v", c.LogPrefix, err)
			return fmt.Errorf("Controller APIMonitor RemoveInboundTag: %w", err)
		}

		c.nodeInfo = newNodeInfo
		c.Tag = c.buildNodeTag()
		c.LogPrefix = c.logPrefix()

		if newNodeInfo.RelayType == 1 && newNodeInfo.RelayNodeID > 0 {
			newRelayNodeInfo, err := c.client.GetTransitNode()
			if err != nil {
				log.Printf("%s Controller APIMonitor GetTransitNode: %v", c.LogPrefix, err)
				return fmt.Errorf("Controller APIMonitor GetTransitNode: %w", err)
			}
			c.relaynodeInfo = newRelayNodeInfo
			c.RelayTag = c.buildRNodeTag()

			if err := c.nodeManager.AddRelayTag(newRelayNodeInfo, c.RelayTag, c.Tag, newSubscriptionInfo); err != nil {
				log.Printf("%s Controller APIMonitor AddRelayTag: %v", c.LogPrefix, err)
				return fmt.Errorf("Controller APIMonitor AddRelayTag: %w", err)
			}
			c.Relay = true
		}

		if err := c.nodeManager.AddTag(newNodeInfo, c.Tag, c.config); err != nil {
			log.Printf("%s Controller APIMonitor AddInboundTag: %v", c.LogPrefix, err)
			return fmt.Errorf("Controller APIMonitor AddInboundTag: %w", err)
		}

		if err := c.nodeManager.AddBlackHoleRuleTag(newNodeInfo, c.Tag); err != nil {
			log.Printf("%s Controller APIMonitor AddBlackHoleRuleTag: %v", c.LogPrefix, err)
		}

		if err := c.subManager.AddNewSubscription(newSubscriptionInfo, newNodeInfo, c.Tag); err != nil {
			log.Printf("%s Controller APIMonitor AddNewSubscription: %v", c.LogPrefix, err)
		}

		if oldTag != c.Tag {
			if err := c.nodeManager.DeleteInboundLimiter(oldTag); err != nil {
				log.Printf("%s Controller APIMonitor DeleteInboundLimiter: %v", c.LogPrefix, err)
				return nil
			}
			if err := c.nodeManager.AddInboundLimiter(c.Tag, newNodeInfo.UpdateTime, newNodeInfo.SpeedLimit, newNodeInfo.IgnoreIPs, newSubscriptionInfo); err != nil {
				log.Printf("%s Controller APIMonitor AddInboundLimiter: %v", c.LogPrefix, err)
			}
		} else {
			if err := c.nodeManager.UpdateNodeInfo(c.Tag, newNodeInfo.SpeedLimit, newNodeInfo.IgnoreIPs); err != nil {
				log.Printf("%s Controller APIMonitor UpdateNodeInfo: %v", c.LogPrefix, err)
			}

			deleted, added, modified := subscription.Compare(c.subscriptionList, newSubscriptionInfo)
			if len(deleted) > 0 {
				deletedEmail := subscription.FormatEmails(deleted, oldTag)
				c.nodeManager.DeleteSubscriptionBuckets(oldTag, deletedEmail)
			}
			if len(added) > 0 {
				if err := c.nodeManager.UpdateInboundLimiter(oldTag, &added); err != nil {
					log.Printf("%s Error updating limiter for new subscriptions: %v", c.LogPrefix, err)
				}
			}
			if len(modified) > 0 {
				deletedEmail := subscription.FormatEmails(modified, oldTag)
				c.nodeManager.DeleteSubscriptionBuckets(oldTag, deletedEmail)
				if err := c.nodeManager.UpdateInboundLimiter(oldTag, &modified); err != nil {
					log.Printf("%s Error updating limiter for modified subscriptions: %v", c.LogPrefix, err)
				}
			}
		}

		newInterval := c.pollInterval()
		if c.currentPollInterval != newInterval {
			for _, tag := range []string{"node", "subscriptions"} {
				if t := c.taskManager.GetTask(tag); t != nil {
					if err := t.RestartWithInterval(newInterval); err != nil {
						log.Printf("%s Failed to restart %s task: %v", c.LogPrefix, tag, err)
					} else {
						log.Printf("%s %s task restarted with interval %v", c.LogPrefix, tag, newInterval)
					}
				}
			}
			c.currentPollInterval = newInterval
			select {
			case c.intervalChangeCh <- newInterval:
			default:
			}
		}

	} else {
		if nodeInfoChanged {
			c.nodeInfo = newNodeInfo
			if err := c.nodeManager.UpdateNodeInfo(c.Tag, newNodeInfo.SpeedLimit, newNodeInfo.IgnoreIPs); err != nil {
				log.Printf("%s Controller APIMonitor UpdateNodeInfo: %v", c.LogPrefix, err)
			}

			newInterval := c.pollInterval()
			if c.currentPollInterval != newInterval {
				for _, tag := range []string{"node", "subscriptions"} {
					if t := c.taskManager.GetTask(tag); t != nil {
						if err := t.RestartWithInterval(newInterval); err != nil {
							log.Printf("%s Failed to restart %s task: %v", c.LogPrefix, tag, err)
						} else {
							log.Printf("%s %s task restarted with interval %v", c.LogPrefix, tag, newInterval)
						}
					}
				}
				c.currentPollInterval = newInterval
				select {
				case c.intervalChangeCh <- newInterval:
				default:
				}
			}
		}

		if subscriptionChanged {
			if newNodeInfo.RelayType == 1 && newNodeInfo.RelayNodeID > 0 && !c.Relay {
				newRelayNodeInfo, err := c.client.GetTransitNode()
				if err != nil {
					log.Printf("%s Controller APIMonitor GetTransitNode: %v", c.LogPrefix, err)
					return fmt.Errorf("Controller APIMonitor GetTransitNode: %w", err)
				}
				c.relaynodeInfo = newRelayNodeInfo
				c.RelayTag = c.buildRNodeTag()

				if err := c.nodeManager.AddRelayTag(newRelayNodeInfo, c.RelayTag, c.Tag, newSubscriptionInfo); err != nil {
					log.Printf("%s Controller APIMonitor AddRelayTag: %v", c.LogPrefix, err)
					return fmt.Errorf("Controller APIMonitor AddRelayTag: %w", err)
				}
				c.Relay = true
			}

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
	}

	c.subscriptionList = newSubscriptionInfo
	return nil
}

func (c *Controller) serverMonitor() error {
	s := monitor.Collect()
	status := &api.ServerStatus{
		CPU:         s.CPU,
		MemUsed:     s.MemUsed,
		MemTotal:    s.MemTotal,
		SwapUsed:    s.SwapUsed,
		SwapTotal:   s.SwapTotal,
		DiskUsed:    s.DiskUsed,
		DiskTotal:   s.DiskTotal,
		Load1:       s.Load1,
		Load5:       s.Load5,
		Load15:      s.Load15,
		NetInSpeed:  s.NetInSpeed,
		NetOutSpeed: s.NetOutSpeed,
		Uptime:      s.Uptime,
	}
	if c.pusher != nil {
		if err := c.nodePusher()("server_status", status); err != nil {
			log.Printf("%s serverMonitor push failed: %v", c.LogPrefix, err)
		} else {
			log.Printf("%s Pushed server status via Reverb", c.LogPrefix)
		}
	} else {
		if err := c.client.ReportServerStatus(status); err != nil {
			log.Printf("%s serverMonitor: %v", c.LogPrefix, err)
		}
	}
	return nil
}

func (c *Controller) certMonitor() error {
	if c.nodeInfo.TlsSettings == nil {
		return nil
	}
	switch c.nodeInfo.TlsSettings.CertMode {
	case "dns":
		pn := c.config.CertConfig.Provider
		if c.nodeInfo.TlsSettings.DnsProvider != "" {
			pn = c.nodeInfo.TlsSettings.DnsProvider
		}
		if pn == "" {
			return fmt.Errorf("cert dns provider name is required")
		}
		lego, err := cert.NewForNode(c.config.CertConfig, pn)
		if err != nil {
			return err
		}
		if _, _, _, err := lego.RenewCert(c.nodeInfo.TlsSettings.CertMode, c.nodeInfo.TlsSettings.ServerName, c.nodeInfo.TlsSettings.CertEmail); err != nil {
			log.Printf("%s cert renew failed: %v", c.LogPrefix, err)
		}
	case "http", "tls":
		lego, err := cert.New(c.config.CertConfig)
		if err != nil {
			return fmt.Errorf("cert init: %w", err)
		}
		if _, _, _, err := lego.RenewCert(c.nodeInfo.TlsSettings.CertMode, c.nodeInfo.TlsSettings.ServerName, c.nodeInfo.TlsSettings.CertEmail); err != nil {
			log.Printf("%s cert renew failed: %v", c.LogPrefix, err)
		}
	}
	return nil
}

func (c *Controller) logPrefix() string {
	return fmt.Sprintf("[%s] %s(XMRay NodeID=%d)", c.clientInfo.APIHost, c.nodeInfo.NodeType, c.nodeInfo.NodeID)
}

func (c *Controller) buildNodeTag() string {
	return fmt.Sprintf("%s_%s_%d", c.nodeInfo.NodeType, c.nodeInfo.ListeningPort, c.nodeInfo.NodeID)
}

func (c *Controller) nodePusher() func(string, any) error {
	if c.pusher == nil {
		return nil
	}
	nodeID := c.clientInfo.NodeID
	push := c.pusher
	return func(event string, data any) error {
		return push(event, map[string]any{
			"node_id": nodeID,
			"data":    data,
		})
	}
}

func (c *Controller) buildRNodeTag() string {
	return fmt.Sprintf("Relay_%s_%d_%d", c.relaynodeInfo.NodeType, c.relaynodeInfo.ListeningPort, c.relaynodeInfo.NodeID)
}
