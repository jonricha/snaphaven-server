package main

import (
	_ "embed"
	"log"
	"os"

	"github.com/energye/systray"
)

//go:embed icon.ico
var trayIconBytes []byte

type TrayApp struct {
	serverMgr   *ServerManager
	setupServer *SetupServer
}

func NewTrayApp(sm *ServerManager, ss *SetupServer) *TrayApp {
	return &TrayApp{
		serverMgr:   sm,
		setupServer: ss,
	}
}

func (t *TrayApp) Run() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Notice: System tray unavailable (%v). Running as background daemon.", r)
			select {}
		}
	}()
	systray.Run(t.onReady, t.onExit)
}

func (t *TrayApp) onReady() {
	systray.SetTitle("PhotoSync")
	systray.SetTooltip("PhotoSync Server")

	// Set system tray icon from embedded icon.ico
	systray.SetIcon(trayIconBytes)

	mStatus := systray.AddMenuItem("Status: Server Running", "Server Status")
	mStatus.Disable()

	mQR := systray.AddMenuItem("📱 Show Pairing QR Code", "Open pairing page in browser")
	mDashboard := systray.AddMenuItem("🌐 Open Web Dashboard", "Open management dashboard in browser")
	mSettings := systray.AddMenuItem("⚙️ Settings", "Open settings tab in browser")
	mLogs := systray.AddMenuItem("📋 View Live Logs", "Open logs tab in browser")

	systray.AddSeparator()
	mToggle := systray.AddMenuItem("Pause Server", "Toggle gRPC sync server")
	mQuit := systray.AddMenuItem("Quit PhotoSync", "Quit the PhotoSync server application")

	mQR.Click(func() {
		OpenBrowser(t.setupServer.ServerURL + "/#pairing")
	})

	mDashboard.Click(func() {
		OpenBrowser(t.setupServer.ServerURL + "/")
	})

	mSettings.Click(func() {
		OpenBrowser(t.setupServer.ServerURL + "/#settings")
	})

	mLogs.Click(func() {
		OpenBrowser(t.setupServer.ServerURL + "/#logs")
	})

	mToggle.Click(func() {
		if t.serverMgr.IsRunning() {
			t.serverMgr.Stop()
			mStatus.SetTitle("Status: Server Stopped")
			mToggle.SetTitle("Start Server")
		} else {
			if err := t.serverMgr.Start(); err == nil {
				mStatus.SetTitle("Status: Server Running")
				mToggle.SetTitle("Pause Server")
			}
		}
	})

	mQuit.Click(func() {
		log.Printf("Quit requested from System Tray.")
		systray.Quit()
	})
}

func (t *TrayApp) onExit() {
	t.serverMgr.Stop()
	log.Printf("PhotoSync Server safely exited.")
	os.Exit(0)
}
