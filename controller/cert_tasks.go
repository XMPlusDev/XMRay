package controller

import (
	"log"
	"time"

	"github.com/xmplusdev/xmray/helper/scheduler"
)

const (
	taskCertRenew      = "cert_renew"
	taskCertSelfSigned = "cert_self_signed"

	selfSignedCheckInterval = 24 * time.Hour
)

func (c *Controller) certTasksWanted() (renew bool, selfSigned bool) {
	info := c.nodeInfo
	if info == nil || info.SecurityType != "tls" || info.TlsSettings == nil {
		return false, false
	}
	switch info.TlsSettings.CertMode {
	case "dns", "http", "tls":
		return true, false
	case "self-signed":
		return false, true
	}
	return false, false
}

func (c *Controller) addCertTasks() {
	renew, selfSigned := c.certTasksWanted()
	if renew {
		c.taskManager.Add(scheduler.NewWithDelay(c.LogPrefix, taskCertRenew, c.certRenewInterval(), c.certMonitor))
	}
	if selfSigned {
		c.taskManager.Add(scheduler.NewWithDelay(c.LogPrefix, taskCertSelfSigned, selfSignedCheckInterval, c.selfSignedCertMonitor))
	}
}

func (c *Controller) syncCertTasks() {
	renew, selfSigned := c.certTasksWanted()
	c.syncTask(taskCertRenew, renew, c.certRenewInterval(), c.certMonitor)
	c.syncTask(taskCertSelfSigned, selfSigned, selfSignedCheckInterval, c.selfSignedCertMonitor)
}

func (c *Controller) certRenewInterval() time.Duration {
	return c.pollInterval() * 60
}

func (c *Controller) syncTask(tag string, wanted bool, interval time.Duration, fn func() error) {
	t := c.taskManager.GetTask(tag)
	switch {
	case !wanted && t != nil:
		if err := c.taskManager.RemoveTask(tag); err != nil {
			log.Printf("%s Failed to stop %s task: %v", c.LogPrefix, tag, err)
			return
		}
		log.Printf("%s %s task stopped (cert mode changed)", c.LogPrefix, tag)

	case wanted && t == nil:
		task := scheduler.NewWithDelay(c.LogPrefix, tag, interval, fn)
		if err := c.taskManager.AddAndStart(task); err != nil {
			log.Printf("%s Failed to start %s task: %v", c.LogPrefix, tag, err)
			return
		}
		log.Printf("%s %s task started (cert mode changed)", c.LogPrefix, tag)

	case wanted && t != nil && t.GetInterval() != interval:
		if err := t.RestartWithInterval(interval); err != nil {
			log.Printf("%s Failed to restart %s task: %v", c.LogPrefix, tag, err)
			return
		}
		log.Printf("%s %s task restarted with interval %v", c.LogPrefix, tag, interval)
	}
}