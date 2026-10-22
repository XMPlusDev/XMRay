package scheduler

import (
	"sync"
	"time"
)

type Periodic struct {
	Interval time.Duration
	Execute  func() error
	delay    bool

	access  sync.Mutex
	timer   *time.Timer
	running bool
	gen     uint64 
}

func (t *Periodic) GetInterval() time.Duration {
	t.access.Lock()
	defer t.access.Unlock()
	return t.Interval
}

func (t *Periodic) SetInterval(d time.Duration) {
	t.access.Lock()
	defer t.access.Unlock()
	t.Interval = d
}

func (t *Periodic) schedule(d time.Duration, gen uint64) {
	t.timer = time.AfterFunc(d, func() { t.run(gen) })
}

func (t *Periodic) run(gen uint64) {
	t.access.Lock()
	if !t.running || t.gen != gen { 
		t.access.Unlock()
		return
	}
	t.access.Unlock()

	t.Execute() //nolint:errcheck — callers log their own errors

	t.access.Lock()
	defer t.access.Unlock()
	if t.running && t.gen == gen {
		t.schedule(t.Interval, gen)
	}
}

func (t *Periodic) Start() error {
	t.access.Lock()
	defer t.access.Unlock()

	if t.running {
		return nil
	}
	t.running = true
	t.gen++

	if t.delay {
		t.schedule(t.Interval, t.gen)
	} else {
		t.schedule(0, t.gen)
	}
	return nil
}

func (t *Periodic) Close() error {
	t.access.Lock()
	defer t.access.Unlock()

	t.running = false
	t.gen++ // invalidate any in-flight run and any timer that already fired
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	return nil
}