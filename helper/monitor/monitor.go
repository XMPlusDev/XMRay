package monitor

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

var startTime = time.Now()

func init() {
	cpu.Percent(500*time.Millisecond, false)
	collectNetSpeed()
}

type Status struct {
	Uptime uint64

	CPU        float64
	CPUPerCore []float64

	Load1  float64
	Load5  float64
	Load15 float64

	MemTotal  uint64
	MemUsed   uint64
	SwapTotal uint64
	SwapUsed  uint64

	DiskTotal uint64
	DiskUsed  uint64

	// Bytes per second; -1 means unavailable (first sample or counter reset).
	NetInSpeed  float64
	NetOutSpeed float64
}

var (
	netMu       sync.Mutex
	netPrevRecv uint64
	netPrevSent uint64
	netPrevTime time.Time
	netHasBase  bool
)

func skipInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"lo", "docker", "veth", "br-", "virbr", "vnet", "tun", "tap"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func collectNetSpeed() (inSpeed, outSpeed float64) {
	counters, err := net.IOCounters(true)
	if err != nil {
		log.Printf("[monitor] failed to get network counters: %v", err)
		return -1, -1
	}

	var totalRecv, totalSent uint64
	for _, c := range counters {
		if skipInterface(c.Name) {
			continue
		}
		totalRecv += c.BytesRecv
		totalSent += c.BytesSent
	}

	now := time.Now()

	netMu.Lock()
	defer netMu.Unlock()

	if !netHasBase {
		netPrevRecv, netPrevSent, netPrevTime, netHasBase = totalRecv, totalSent, now, true
		return -1, -1
	}

	elapsed := now.Sub(netPrevTime).Seconds()
	if elapsed <= 0 {
		return -1, -1
	}

	if totalRecv < netPrevRecv || totalSent < netPrevSent {
		netPrevRecv, netPrevSent, netPrevTime = totalRecv, totalSent, now
		return -1, -1
	}

	inSpeed = float64(totalRecv-netPrevRecv) / elapsed
	outSpeed = float64(totalSent-netPrevSent) / elapsed
	netPrevRecv, netPrevSent, netPrevTime = totalRecv, totalSent, now
	return inSpeed, outSpeed
}

func Collect() Status {
	var s Status

	s.Uptime = uint64(time.Since(startTime).Seconds())

	if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
		s.CPU = pct[0]
	} else if err != nil {
		log.Printf("[monitor] failed to get CPU usage: %v", err)
	}

	if perCore, err := cpu.Percent(0, true); err == nil {
		s.CPUPerCore = perCore
	}

	if avg, err := load.Avg(); err == nil {
		s.Load1 = avg.Load1
		s.Load5 = avg.Load5
		s.Load15 = avg.Load15
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		s.MemTotal = vm.Total
		s.MemUsed = vm.Used
	}

	if swap, err := mem.SwapMemory(); err == nil {
		s.SwapTotal = swap.Total
		s.SwapUsed = swap.Used
	}

	if d, err := disk.Usage("/"); err == nil {
		s.DiskTotal = d.Total
		s.DiskUsed = d.Used
	}

	s.NetInSpeed, s.NetOutSpeed = collectNetSpeed()

	return s
}
