package main

import (
	"runtime"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

type trayController struct {
	ready    chan struct{}
	done     chan struct{}
	doneOnce sync.Once
	show     func()
	settings func()
	exit     func()
}

func newTrayController(show, settings, exit func()) *trayController {
	return &trayController{ready: make(chan struct{}), done: make(chan struct{}), show: show, settings: settings, exit: exit}
}

func (t *trayController) start() {
	go func() {
		// Windows 托盘消息循环独占自己的系统线程，不占用 Wails 窗口线程。
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		systray.Run(func() {
			systray.SetIcon(trayIcon)
			systray.SetTooltip("桌面提醒 · 右键打开菜单")
			show := systray.AddMenuItem("打开主窗口", "查看倒计时与提醒设置")
			settings := systray.AddMenuItem("系统通知设置", "打开 Windows 通知设置")
			systray.AddSeparator()
			exit := systray.AddMenuItem("退出应用", "停止提醒并退出桌面提醒")
			close(t.ready)
			for {
				select {
				case <-show.ClickedCh:
					t.show()
				case <-settings.ClickedCh:
					t.settings()
				case <-exit.ClickedCh:
					t.exit()
					return
				case <-t.done:
					return
				}
			}
		}, func() { t.doneOnce.Do(func() { close(t.done) }) })
	}()
}

func (t *trayController) isReady() bool {
	select {
	case <-t.done:
		return false
	default:
	}
	select {
	case <-t.ready:
		return true
	default:
		return false
	}
}

func (t *trayController) stop() {
	select {
	case <-t.done:
		return
	case <-t.ready:
		systray.Quit()
	case <-time.After(2 * time.Second):
		return
	}
	// 给托盘消息循环时间移除图标，避免退出后残留图标。
	select {
	case <-t.done:
	case <-time.After(2 * time.Second):
	}
}
