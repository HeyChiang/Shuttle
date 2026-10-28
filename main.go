package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"shuttle/internal/sshcore"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Registering the event payload types gives the binding generator enough
	// information to produce a typed JS/TS listener API.
	application.RegisterEvent[sshcore.DataEvent](sshcore.EventData)
	application.RegisterEvent[sshcore.StatusEvent](sshcore.EventStatus)
	application.RegisterEvent[sshcore.ClosedEvent](sshcore.EventClosed)
	application.RegisterEvent[sshcore.ExecEvent](sshcore.EventExec)
	application.RegisterEvent[sshcore.TransferEvent](sshcore.EventTransfer)
}

func main() {
	sshService, err := NewSSHService()
	if err != nil {
		log.Fatalf("初始化 SSH 服务失败: %v", err)
	}

	app := application.New(application.Options{
		Name:        "Shuttle",
		Description: "多主机 SSH 批量管理工具",
		Services: []application.Service{
			application.NewService(sshService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Shuttle - SSH 批量管理",
		Width:     1360,
		Height:    860,
		MinWidth:  1080,
		MinHeight: 680,
		// 去掉系统标题栏与边框，改为前端自绘标题栏。
		Frameless: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		// 与前端 Termius 风格底色 #15171c 保持一致，避免窗口出现瞬间的白色闪屏。
		BackgroundColour: application.NewRGB(21, 23, 28),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}