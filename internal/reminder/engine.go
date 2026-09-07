package reminder

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Notifier interface {
	Notify(context.Context, string, string) error
}

type State struct {
	Config            Config `json:"config"`
	Running           bool   `json:"running"`
	Started           bool   `json:"started"`
	RemainingMS       int64  `json:"remainingMs"`
	NextAt            int64  `json:"nextAt"`
	LastNotification  int64  `json:"lastNotification"`
	NotificationCount int    `json:"notificationCount"`
	ConfigWarning     string `json:"configWarning"`
	NotificationError string `json:"notificationError"`
}

type Engine struct {
	mu                sync.Mutex
	config            Config
	path              string
	notifier          Notifier
	now               func() time.Time
	running           bool
	started           bool
	closed            bool
	next              time.Time
	remaining         time.Duration
	last              time.Time
	count             int
	warning           string
	notificationError string
	testing           bool
	ctx               context.Context
	cancel            context.CancelFunc
	activeCancel      context.CancelFunc
	runOnce           sync.Once
	done              chan struct{}
}

func NewEngine(path string, notifier Notifier) *Engine {
	config, err := loadConfig(path)
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{config: config, path: path, notifier: notifier,
		now:       func() time.Time { return time.Now().Round(0) },
		remaining: time.Duration(config.IntervalMinutes) * time.Minute,
		ctx:       ctx, cancel: cancel, done: make(chan struct{})}
	if err != nil {
		e.warning = err.Error()
	}
	return e
}

func (e *Engine) Run(parent context.Context) {
	e.runOnce.Do(func() {
		go func() {
			defer close(e.done)
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-parent.Done():
					e.Close()
					return
				case <-e.ctx.Done():
					return
				case <-ticker.C:
					e.checkDue()
				}
			}
		}()
	})
}

func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	e.running = false
	e.cancel()
	if e.activeCancel != nil {
		e.activeCancel()
	}
}

func (e *Engine) stateLocked() State {
	remaining := e.remaining
	var nextAt int64
	if e.running {
		remaining = e.next.Sub(e.now())
		nextAt = e.next.UnixNano() / int64(time.Millisecond)
	}
	if remaining < 0 {
		remaining = 0
	}
	var last int64
	if !e.last.IsZero() {
		last = e.last.UnixNano() / int64(time.Millisecond)
	}
	return State{Config: e.config, Running: e.running, Started: e.started,
		RemainingMS: int64(remaining / time.Millisecond), NextAt: nextAt,
		LastNotification: last, NotificationCount: e.count,
		ConfigWarning: e.warning, NotificationError: e.notificationError}
}

func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateLocked()
}

func (e *Engine) saveLocked(config Config) error {
	if e.closed {
		return errors.New("应用正在退出")
	}
	config, err := config.Validate()
	if err != nil {
		return err
	}
	if e.running && config != e.config {
		return errors.New("请先暂停，再修改提醒设置")
	}
	if err = saveConfig(e.path, config); err != nil {
		return fmt.Errorf("保存设置失败：%w", err)
	}
	if config.IntervalMinutes != e.config.IntervalMinutes {
		e.remaining = time.Duration(config.IntervalMinutes) * time.Minute
		e.started = false
	}
	e.config = config
	e.warning = ""
	return nil
}

func (e *Engine) SaveConfig(config Config) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.saveLocked(config)
	return e.stateLocked(), err
}

func (e *Engine) Start(config Config) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.saveLocked(config); err != nil {
		return e.stateLocked(), err
	}
	if !e.running {
		if e.remaining <= 0 {
			e.remaining = time.Duration(e.config.IntervalMinutes) * time.Minute
		}
		e.next = e.now().Add(e.remaining)
		e.running, e.started = true, true
	}
	return e.stateLocked(), nil
}

func (e *Engine) Pause() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		e.remaining = e.next.Sub(e.now())
		if e.remaining < time.Millisecond {
			e.remaining = time.Millisecond
		}
		e.running = false
		if e.activeCancel != nil {
			e.activeCancel()
		}
	}
	return e.stateLocked()
}

func (e *Engine) checkDue() {
	e.mu.Lock()
	now := e.now()
	if e.closed || !e.running || now.Before(e.next) {
		e.mu.Unlock()
		return
	}
	content := e.config.Content
	// 休眠唤醒后最多补一次，从当前时刻开始下一轮，避免集中补发。
	e.remaining = time.Duration(e.config.IntervalMinutes) * time.Minute
	e.next = now.Add(e.remaining)
	ctx, cancel := context.WithCancel(e.ctx)
	e.activeCancel = cancel
	e.mu.Unlock()
	err := e.notifier.Notify(ctx, "桌面提醒", content)
	e.mu.Lock()
	if ctx.Err() == nil {
		e.recordLocked(err)
	}
	e.activeCancel = nil
	e.mu.Unlock()
	cancel()
}

func (e *Engine) recordLocked(err error) {
	if err != nil {
		e.notificationError = err.Error()
		return
	}
	e.notificationError = ""
	e.last = e.now()
	e.count++
}

func (e *Engine) TestNotification(config Config) error {
	config, err := config.Validate()
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return errors.New("应用正在退出")
	}
	if e.testing {
		e.mu.Unlock()
		return errors.New("测试通知正在发送，请稍候")
	}
	e.testing = true
	e.mu.Unlock()
	err = e.notifier.Notify(e.ctx, "桌面提醒 · 无声测试", config.Content)
	e.mu.Lock()
	e.testing = false
	if e.ctx.Err() == nil {
		e.recordLocked(err)
	}
	e.mu.Unlock()
	return err
}
