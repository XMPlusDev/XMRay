package instance

import (
	"log"
	"time"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/helper/monitor"
	"github.com/xmplusdev/xmray/helper/scheduler"
)

const serverStatusReportInterval = 5 * time.Second

func (i *Instance) startServerStatusTask(
	client *api.Client,
) {
	task := scheduler.New(
		"[ServerStatus]",
		"server_status",
		&scheduler.Periodic{
			Interval: serverStatusReportInterval,
			Execute: func() error {
				return i.reportServerStatus(client)
			},
		},
	)

	i.serverStatusTask = task

	go func() {
		if err := task.Start(); err != nil {
			log.Printf("[ServerStatus] Failed to start: %v", err)
		}
	}()
}

func (i *Instance) reportServerStatus(client *api.Client) error {
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

	i.reverbMu.Lock()
	pusher := i.currentPusher
	i.reverbMu.Unlock()

	if pusher != nil {
		payload := &api.ServerStatusPayload{
			ServerID: i.instanceConfig.ApiConfig.ServerID,
			Data:     status,
		}
		if err := pusher("server_status", payload); err == nil {
			return nil
		}
	}

	if err := client.ReportServerStatus(status); err != nil {
		log.Printf("[ServerStatus] Report failed: %v", err)
	}
	return nil
}

func (i *Instance) updateServerStatusInterval(_ time.Duration) {}
