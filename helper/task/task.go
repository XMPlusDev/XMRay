package task

import (
	"log"
	"sync"
	"time"
)

type PeriodicTask struct {
	LogTag   string
	Tag      string
	*Periodic
	mu       sync.Mutex
	running  bool
}

func New(logTag string, tag string, periodic *Periodic) *PeriodicTask {
	return &PeriodicTask{
		LogTag:   logTag,
		Tag:      tag,
		Periodic: periodic,
		running:  false,
	}
}

func NewWithInterval(logTag string, tag string, interval time.Duration, execute func() error) *PeriodicTask {
	return &PeriodicTask{
		LogTag: logTag,
		Tag: tag,
		Periodic: &Periodic{
			Interval: interval,
			Execute:  execute,
		},
		running: false,
	}
}

func NewWithDelay(logTag string, tag string, interval time.Duration, execute func() error) *PeriodicTask {
	return &PeriodicTask{
		LogTag: logTag,
		Tag:    tag,
		Periodic: &Periodic{
			Interval: interval,
			Execute:  execute,
			delay:    true,
		},
		running: false,
	}
}

func (pt *PeriodicTask) Start() error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.running {
		return nil
	}

	if pt.Periodic != nil {
		err := pt.Periodic.Start()
		if err == nil {
			pt.running = true
		}
		return err
	}
	return nil
}

func (pt *PeriodicTask) Close() error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if !pt.running {
		return nil
	}

	if pt.Periodic != nil {
		err := pt.Periodic.Close()
		if err == nil {
			pt.running = false
		}
		return err
	}
	return nil
}

func (pt *PeriodicTask) IsRunning() bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.running
}

func (pt *PeriodicTask) Restart() error {
	if err := pt.Close(); err != nil {
		return err
	}
	return pt.Start()
}

type Manager struct {
	tasks []*PeriodicTask
	mu    sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{
		tasks: make([]*PeriodicTask, 0),
	}
}

func (m *Manager) Add(task *PeriodicTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = append(m.tasks, task)
}

func (m *Manager) StartAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.tasks {
		if err := t.Start(); err != nil {
			log.Printf("%s Failed to start task %s: %v", t.LogTag, t.Tag, err)
			return err
		}
		log.Printf("%s Task %s started", t.LogTag, t.Tag)
	}
	return nil
}

func (m *Manager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for _, t := range m.tasks {
		if err := t.Close(); err != nil {
			log.Printf("Failed to close task %s: %v", t.Tag, err)
			lastErr = err
		}
	}
	return lastErr
}

func (m *Manager) GetTask(tag string) *PeriodicTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, t := range m.tasks {
		if t.Tag == tag {
			return t
		}
	}
	return nil
}

func (m *Manager) RemoveTask(tag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, t := range m.tasks {
		if t.Tag == tag {
			if err := t.Close(); err != nil {
				return err
			}
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tasks)
}