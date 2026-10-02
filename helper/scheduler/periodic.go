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
}

func (t *Periodic) run() {
	t.Execute() //nolint:errcheck — callers log their own errors

	t.access.Lock()
	defer t.access.Unlock()
	if t.running {
		t.timer = time.AfterFunc(t.Interval, t.run)
	}
}

func (t *Periodic) Start() error {
	t.access.Lock()
	defer t.access.Unlock()

	if t.running {
		return nil
	}
	t.running = true

	if t.delay {
		t.timer = time.AfterFunc(t.Interval, t.run)
	} else {
		t.timer = time.AfterFunc(0, t.run)
	}
	return nil
}

func (t *Periodic) Close() error {
	t.access.Lock()
	defer t.access.Unlock()

	t.running = false
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	return nil
}
