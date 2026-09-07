package main

import (
	"context"
	"errors"
	"sync"

	"desktop-reminder/internal/reminder"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 只暴露本地桌面绑定；程序不提供业务 HTTP 接口。
type App struct {
	mu            sync.RWMutex
	ctx           context.Context
	engine        *reminder.Engine
	tray          *trayController
	exitRequested bool
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ctx = ctx
	a.engine = reminder.NewEngine(reminder.DefaultConfigPath(), reminder.NewNotifier())
	a.engine.Run(ctx)
	a.tray = newTrayController(a.showMainWindow, func() { a.OpenNotificationSettings() }, a.exitApplication)
	a.tray.start()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.RLock()
	engine, tray := a.engine, a.tray
	a.mu.RUnlock()
	if engine != nil {
		engine.Close()
	}
	if tray != nil {
		tray.stop()
	}
}

func (a *App) showExisting(data options.SecondInstanceData) {
	a.showMainWindow()
}

func (a *App) showMainWindow() {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ctx != nil {
		runtime.WindowUnminimise(a.ctx)
		runtime.WindowShow(a.ctx)
	}
}

// 普通关闭只隐藏主窗口。托盘退出通过独立标记完成真正关闭。
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.RLock()
	exitRequested, tray := a.exitRequested, a.tray
	a.mu.RUnlock()
	if exitRequested || tray == nil || !tray.isReady() {
		return false
	}
	runtime.WindowHide(ctx)
	return true
}

func (a *App) exitApplication() {
	a.mu.Lock()
	a.exitRequested = true
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.Quit(ctx)
	}
}

// 只打开本机设置页面，不改变通知权限或系统静音设置。
func (a *App) OpenNotificationSettings() error {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return errors.New("应用正在初始化，请稍后重试")
	}
	runtime.BrowserOpenURL(ctx, "ms-settings:notifications")
	return nil
}

func (a *App) current() (*reminder.Engine, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.engine == nil {
		return nil, errors.New("应用正在初始化，请稍后重试")
	}
	return a.engine, nil
}

func (a *App) GetState() (reminder.State, error) {
	e, err := a.current()
	if err != nil {
		return reminder.State{}, err
	}
	return e.State(), nil
}

func (a *App) SaveConfig(config reminder.Config) (reminder.State, error) {
	e, err := a.current()
	if err != nil {
		return reminder.State{}, err
	}
	return e.SaveConfig(config)
}

func (a *App) Start(config reminder.Config) (reminder.State, error) {
	e, err := a.current()
	if err != nil {
		return reminder.State{}, err
	}
	return e.Start(config)
}

func (a *App) Pause() (reminder.State, error) {
	e, err := a.current()
	if err != nil {
		return reminder.State{}, err
	}
	return e.Pause(), nil
}

func (a *App) TestNotification(config reminder.Config) error {
	e, err := a.current()
	if err != nil {
		return err
	}
	return e.TestNotification(config)
}
