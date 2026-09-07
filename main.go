package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed frontend/index.html frontend/style.css frontend/app.js
var assets embed.FS

//go:embed build/tray.ico
var trayIcon []byte

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title: "桌面提醒",
		Width: 900, Height: 740, MinWidth: 740, MinHeight: 690,
		BackgroundColour: options.NewRGB(246, 247, 243),
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "c266bb19-d995-4ec4-83d0-1c05c6fd5b30",
			OnSecondInstanceLaunch: app.showExisting,
		},
		Windows: &windows.Options{
			Theme:            windows.Light,
			DisablePinchZoom: true,
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
