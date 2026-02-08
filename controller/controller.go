package controller

import (
	"fmt"
	"log"
	"reflect"
	"time"
	
	"github.com/xtls/xray-core/core"
	
	"github.com/xmplusdev/xmplus-server/api"
	"github.com/xmplusdev/xmplus-server/node"
	"github.com/xmplusdev/xmplus-server/subscription"
	"github.com/xmplusdev/xmplus-server/helper/cert"
	"github.com/xmplusdev/xmplus-server/helper/task"
)

type ManagerInterface interface {
	Restart() error
}

type Controller struct {
	server       *core.Instance
	config       *node.Config
	clientInfo   api.ClientInfo
	client       api.API
	nodeInfo     *api.NodeInfo
	relaynodeInfo *api.RelayNodeInfo
	Tag          string
	LogPrefix    string
	RelayTag     string
	Relay        bool
	subscriptionList  *[]api.SubscriptionInfo
	taskManager  *task.Manager
	startAt      time.Time
	manager      ManagerInterface
	nodeManager  *node.Manager 
	subManager   *subscription.Manager
}

// New return a Controller service with default parameters.
func New(server *core.Instance, api api.API, config *node.Config) *Controller {
	controller := &Controller{
		server:      server,
		config:      config,
		client:      api,
		startAt:     time.Now(),
		taskManager: task.NewManager(), 
		nodeManager: node.NewManager(server),
		subManager:  subscription.NewManager(server, api),
	}

	return controller
}

// SetManager sets the manager reference for this controller
func (c *Controller) SetManager(manager ManagerInterface) {
	c.manager = manager
}

// RestartManager restarts the entire manager
func (c *Controller) RestartManager() error {
	if c.manager == nil {
		return fmt.Errorf("manager reference not set")
	}
	
	log.Printf("%s Initiating full manager restart", c.logPrefix())
	return c.manager.Restart()
}

// Start implement the Start() function of the service interface
func (c *Controller) Start() error {
	c.clientInfo = c.client.Describe()
	
	newNodeInfo, err := c.client.GetNodeInfo() 
	if err != nil {
		return err
	}
	c.nodeInfo = newNodeInfo
	c.Tag = c.buildNodeTag()
	
	// Update Subscription
	subscriptionInfo, err := c.client.GetSubscriptionList() 
	if err != nil {
		return err
	}
	c.subscriptionList = subscriptionInfo
	
	c.Relay = false
	// Add new relay tag
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
	
	err = c.nodeManager.AddRuleTag(
		c.nodeInfo, 
		c.Tag,
	)
	if err != nil {
		log.Panic(err)
		return err
	}
	
	// Add new tag
	err = c.nodeManager.AddTag(
		c.nodeInfo, 
		c.Tag, 
		c.config,
	)
	if err != nil {
		log.Panic(err)
		return err
	}
	
	// Add user Subscriptions
	err = c.subManager.AddNewSubscription(
		subscriptionInfo, 
		newNodeInfo,
		c.Tag,
	)
	if err != nil {
		return err
	}
	
	// Add Limiter
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
	
	// Add periodic tasks using the task manager
	c.taskManager.Add(task.NewWithInterval(
		c.LogPrefix,
		"server",
		time.Duration(c.nodeInfo.UpdateTime)*time.Second,
		c.nodeInfoMonitor,
	))
	
	c.taskManager.Add(task.NewWithInterval(
		c.LogPrefix,
		"subscriptions",
		time.Duration(c.nodeInfo.UpdateTime)*time.Second,
		func() error {
			return c.subManager.SubscriptionMonitor(c.subscriptionList, c.Tag, c.LogPrefix)
		},
	))
	
	// Check cert service if needed
	if c.nodeInfo.SecurityType == "tls" { 
		if c.nodeInfo.TlsSettings.CertMode != "none" {
			c.taskManager.Add(task.NewWithInterval(
				c.LogPrefix,
				"cert renew",
				time.Duration(c.nodeInfo.UpdateTime)*time.Second*60,
				c.certMonitor,
			))
		}
	}

	// Start all tasks
	log.Printf("%s Starting %d task schedulers", c.logPrefix(), c.taskManager.Count())
	return c.taskManager.StartAll()
}

func (c *Controller) nodeInfoMonitor() (err error) {
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

    // Update Subscription
	var subscriptionChanged = true
	newSubscriptionInfo, err := c.client.GetSubscriptionList()
	if err != nil {
		if err.Error() == api.SubscriptionNotModified  {
			subscriptionChanged = false
			newSubscriptionInfo = c.subscriptionList
		} else {
			log.Print(err)
			return nil
		}
	}	
	
	var InfoUpdated = false	
	if subscriptionChanged || nodeInfoChanged {
		InfoUpdated = true
	}
	
	if c.Relay && InfoUpdated {
		err := c.nodeManager.RemoveRelayRules(
			c.RelayTag, 
			c.subscriptionList,
		)
		if err != nil {
			log.Print(err)
		}	
	
	    // Remove relay tag
		err = c.nodeManager.RemoveRelayTag(
			c.RelayTag, 
			c.subscriptionList,
		)
		if err != nil {
			return err
		}
		c.Relay = false
	}
	
	// Update new Relay tag
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
	
	// If nodeInfo changed
	if nodeInfoChanged {
		if !reflect.DeepEqual(c.nodeInfo, newNodeInfo) {
			// Remove old tag
			oldTag := c.Tag
			err := c.nodeManager.RemoveTag(oldTag)
			if err != nil {
				log.Print(err)
				return nil
			}
			err = c.nodeManager.RemoveBlockingRules(oldTag)
			if err != nil {
				log.Print(err)
			}
			if c.nodeInfo.NodeType == "Shadowsocks-Plugin" {
				err = c.nodeManager.RemoveTag(fmt.Sprintf("dokodemo-door_%s+1", c.Tag))
			}
			if err != nil {
				log.Print(err)
				return nil
			}
			
			// Add new tag
			c.nodeInfo = newNodeInfo
			c.Tag = c.buildNodeTag()
		
			err = c.nodeManager.AddRuleTag(
				newNodeInfo, 
				c.Tag, 
			)
			if err != nil {
				log.Print(err)
				return nil
			}
		
			err = c.nodeManager.AddTag(newNodeInfo, c.Tag, c.config)
			if err != nil {
				log.Print(err)
				return nil
			}
			//nodeInfoChanged = true
		
			// Remove Old limiter
			err = c.nodeManager.DeleteInboundLimiter(oldTag)
			if err != nil {
				log.Print(err)
				return nil
			}
		} else {
			nodeInfoChanged = false
		}
	}
	
	if nodeInfoChanged {
		err := c.subManager.AddNewSubscription(
			newSubscriptionInfo, 
			newNodeInfo, 
			c.Tag,
		)
		if err != nil {
			log.Print(err)
			return nil
		}
		
		err = c.nodeManager.AddInboundLimiter(
			c.Tag, 
			newNodeInfo.UpdateTime,
			newNodeInfo.SpeedLimit, 
			newSubscriptionInfo, 
			c.config.RedisConfig,
		)
		if err != nil {
			log.Print(err)
			return nil
		}	
	}else {
		if subscriptionChanged {
			deleted, added, modified := subscription.Compare(c.subscriptionList, newSubscriptionInfo)
			
			// Log what changed for debugging
			log.Printf("%s Subscription Monitoring - Deleted: %d, Added: %d, Modified: %d", 
				c.LogPrefix, len(deleted), len(added), len(modified))
			
			// Handle deleted subscriptions
			if len(deleted) > 0 {
				deletedEmail := subscription.FormatEmails(deleted, c.Tag)
				if err := c.subManager.Remove(deletedEmail, c.Tag); err != nil {
					log.Printf("%s Error removing subscriptions: %v", c.LogPrefix, err)
				}
			}
			
			// Handle added subscriptions
			if len(added) > 0 {
				err := c.subManager.AddNewSubscription(&added, c.nodeInfo, c.Tag)
				if err != nil {
					log.Printf("%s Error adding subscriptions: %v", c.LogPrefix, err)
				} else {
					//log.Printf("%s Successfully added %d subscriptions", c.LogPrefix, len(added))
					// Update Limiter for new subscriptions
					log.Printf("%s Updating limiter for %d added subscription(s)", c.LogPrefix, len(added))
					if err := c.nodeManager.UpdateInboundLimiter(c.Tag, &added); err != nil {
						log.Printf("%s Error updating limiter for new subscriptions: %v", c.LogPrefix, err)
					}
				}
			}
			
			// Handle modified subscriptions (properties changed but same ID)
			if len(modified) > 0 {
				log.Printf("%s Updating limiter for %d modified subscription(s)", c.LogPrefix, len(modified))
				// Update Limiter for modified subscriptions without removing/re-adding them
				if err := c.nodeManager.UpdateInboundLimiter(c.Tag, &modified); err != nil {
					log.Printf("%s Error updating limiter for modified subscriptions: %v", c.LogPrefix, err)
				}
			}
		}
	}
	
	c.subscriptionList = newSubscriptionInfo
	return nil
}

// Close implement the Close() function of the service interface
func (c *Controller) Close() error {
	log.Printf("%s Closing %d task schedulers", c.logPrefix(), c.taskManager.Count())
	return c.taskManager.CloseAll()
}

func (c *Controller) certMonitor() error {
	switch c.nodeInfo.TlsSettings.CertMode {
	case "dns", "http":
		lego, err := cert.New(c.config.CertConfig)
		if err != nil {
			log.Print(err)
		}
		_, _, _, err = lego.RenewCert(c.nodeInfo.TlsSettings.CertMode, c.nodeInfo.TlsSettings.CertDomainName)
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