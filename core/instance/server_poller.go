package instance

import (
	"context"
	"log"
	"time"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/controller"
	"github.com/xmplusdev/xmray/node"
	"github.com/xmplusdev/xmray/helper/scheduler"
)

const defaultServerNodePollInterval = 60 * time.Second

func pollDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultServerNodePollInterval
	}
	return time.Duration(seconds) * time.Second
}

func (i *Instance) startServerNodePoller(
	rootClient *api.Client,
	controllerConfig *node.Config,
	pusher func(string, any) error,
	initialPollInterval int,
) {
	current := pollDuration(initialPollInterval)

	var task *scheduler.PeriodicTask
	task = scheduler.NewWithDelay("[ServerPoller]", "server_nodes", current, func() error {
		newInterval := i.syncServerNodes(rootClient, controllerConfig, pusher)
		if newInterval != task.Periodic.Interval {
			log.Printf("[ServerPoller] Poll interval changed: %v → %v", task.Periodic.Interval, newInterval)
			task.RestartWithInterval(newInterval)
			i.updateServerStatusInterval(newInterval)
		}
		return nil
	})

	if err := task.Start(); err != nil {
		log.Printf("[ServerPoller] Failed to start: %v", err)
		return
	}

	i.serverPoller = task

	ctx, cancel := context.WithCancel(context.Background())
	i.reverbCancels = append(i.reverbCancels, cancel)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-i.serverPollTrigger:
				log.Printf("[ServerPoller] server_change received — syncing immediately")
				newInterval := i.syncServerNodes(rootClient, controllerConfig, pusher)
				task.RestartWithInterval(newInterval)
				i.updateServerStatusInterval(newInterval)
			}
		}
	}()
}

func (i *Instance) syncServerNodes(
	rootClient *api.Client,
	controllerConfig *node.Config,
	pusher func(string, any) error,
) time.Duration {
	resp, err := rootClient.GetServerNodes()
	if err != nil {
		log.Printf("[ServerPoller] Failed to fetch node list: %v", err)
		return defaultServerNodePollInterval
	}

	interval := pollDuration(resp.PollInterval)

	i.statusLock.Lock()
	defer i.statusLock.Unlock()

	panelIDs := make(map[int]struct{}, len(resp.Nodes))
	for _, n := range resp.Nodes {
		panelIDs[n.NodeID] = struct{}{}
	}

	for nodeID, t := range i.controllerMap {
		if _, exists := panelIDs[nodeID]; !exists {
			log.Printf("[ServerPoller] Node %d removed — stopping controller", nodeID)
			if svc, ok := t.(controller.ControllerInterface); ok {
				if err := svc.Close(); err != nil {
					log.Printf("[ServerPoller] Error closing controller for node %d: %v", nodeID, err)
				}
				i.Service = removeService(i.Service, svc)
			}
			delete(i.controllerMap, nodeID)
		}
	}

	for _, n := range resp.Nodes {
		if _, exists := i.controllerMap[n.NodeID]; exists {
			continue 
		}
		log.Printf("[ServerPoller] Node %d added — starting controller", n.NodeID)
		nodeClient := rootClient.ForNode(n.NodeID)
		svc := controller.New(i.Server, nodeClient, controllerConfig, i.Dispatcher, pusher)
		if err := svc.Start(); err != nil {
			log.Printf("[ServerPoller] Failed to start controller for node %d: %v", n.NodeID, err)
			continue
		}
		i.Service = append(i.Service, svc)
		i.controllerMap[n.NodeID] = svc
	}

	return interval
}

func removeService(services []controller.ControllerInterface, target controller.ControllerInterface) []controller.ControllerInterface {
	out := services[:0]
	for _, s := range services {
		if s != target {
			out = append(out, s)
		}
	}
	return out
}
