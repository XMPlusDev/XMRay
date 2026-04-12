package task

import (
	"sync"
	"time"
)

type Periodic struct {
	Interval time.Duration
	Execute func() error
	delay    bool 
	access  sync.Mutex
	timer   *time.Timer
	running bool
}

func (t *Periodic) hasClosed() bool {
	t.access.Lock()
	defer t.access.Unlock()

	return !t.running
}

func (t *Periodic) checkedExecute() error {
	if t.hasClosed() {
		return nil
	}

	if err := t.Execute(); err != nil {
		t.access.Lock()
		t.running = false
		t.access.Unlock()
		return err
	}

	t.access.Lock()
	defer t.access.Unlock()

	if !t.running {
		return nil
	}

	t.timer = time.AfterFunc(t.Interval, func() {
		t.checkedExecute()
	})

	return nil
}

func (t *Periodic) Start() error {
	t.access.Lock()
	if t.running {
		t.access.Unlock()
		return nil
	}
	t.running = true

	if t.delay {
		t.timer = time.AfterFunc(t.Interval, func() {
			t.checkedExecute()
		})
		t.access.Unlock()
		return nil
	}

	t.access.Unlock()
	if err := t.checkedExecute(); err != nil {
		t.access.Lock()
		t.running = false
		t.access.Unlock()
		return err
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