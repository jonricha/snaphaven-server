package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/energye/systray"
)

//go:embed icon.ico
var trayIconBytes []byte

type TrayApp struct {
	serverMgr   *ServerManager
	setupServer *SetupServer
	updater     *UpdateManager
}

func NewTrayApp(sm *ServerManager, ss *SetupServer, um *UpdateManager) *TrayApp {
	return &TrayApp{
		serverMgr:   sm,
		setupServer: ss,
		updater:     um,
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
	systray.SetTitle("SnapHaven")
	systray.SetTooltip("SnapHaven Server " + GetFormattedVersion())

	// Set system tray icon from embedded icon.ico
	systray.SetIcon(trayIconBytes)

	mStatus := systray.AddMenuItem("Status: Server Running", "Server Status")
	mStatus.Disable()

	mQR := systray.AddMenuItem("📱 Show Pairing QR Code", "Open pairing page in browser")
	mDashboard := systray.AddMenuItem("🌐 Open Web Dashboard", "Open management dashboard in browser")
	mSettings := systray.AddMenuItem("⚙️ Settings", "Open settings tab in browser")
	mLogs := systray.AddMenuItem("📋 View Live Logs", "Open logs tab in browser")
	mCheckUpdate := systray.AddMenuItem("🔄 Check for Updates...", "Check for software updates")
	mAbout := systray.AddMenuItem("ℹ️ About SnapHaven...", "View software version, author, and build details")

	systray.AddSeparator()
	mToggle := systray.AddMenuItem("Pause Server", "Toggle gRPC sync server")
	mQuit := systray.AddMenuItem("Quit SnapHaven", "Quit the SnapHaven server application")

	if t.updater != nil {
		t.updater.SetUpdateCallback(func(info *UpdateInfo) {
			mCheckUpdate.SetTitle("✨ Install Update (" + info.Version + ")...")
		})
	}

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

	mCheckUpdate.Click(func() {
		if t.updater != nil {
			st := t.updater.GetStatus()
			if st.State == StateUpdateAvailable || st.State == StateReadyToInstall {
				LogEvent("🚀 User initiated update from System Tray menu...")
				go func() {
					if st.State == StateUpdateAvailable {
						err := t.updater.DownloadUpdate()
						if err != nil {
							return
						}
					}
					t.updater.ApplyUpdate()
				}()
				if t.setupServer != nil {
					OpenBrowser(t.setupServer.ServerURL + "/#settings")
				}
				return
			}
		}

		if t.setupServer != nil {
			OpenBrowser(t.setupServer.ServerURL + "/#settings")
		}
		if t.updater != nil {
			go t.updater.CheckForUpdates()
		}
	})

	mAbout.Click(func() {
		go ShowAboutDialog(GetVersion(), GetCommit(), GetBuildTime())
		if t.setupServer != nil {
			OpenBrowser(t.setupServer.ServerURL + "/#about")
		}
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
	log.Printf("SnapHaven Server safely exited.")
	os.Exit(0)
}

// ShowAboutDialog opens a native Windows pop-up dialog with complete software details
func ShowAboutDialog(version, commit, buildTime string) {
	msg := fmt.Sprintf(
		"SnapHaven Server\n"+
			"--------------------------------------------------\n"+
			"Version: %s\n"+
			"Git Commit: %s\n"+
			"Build Time: %s\n"+
			"Author: Jonathan Richardson\n"+
			"License: MIT\n"+
			"Repository: https://github.com/jonricha/snaphaven-server\n"+
			"--------------------------------------------------\n"+
			"Secure mTLS Photo & File Synchronization Server",
		version, commit, buildTime,
	)

	if runtime.GOOS == "windows" {
		user32 := syscall.NewLazyDLL("user32.dll")
		messageBox := user32.NewProc("MessageBoxW")
		captionPtr, _ := syscall.UTF16PtrFromString("About SnapHaven Server")
		textPtr, _ := syscall.UTF16PtrFromString(msg)
		const MB_OK = 0x00000000
		const MB_ICONINFORMATION = 0x00000040
		messageBox.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(captionPtr)), uintptr(MB_OK|MB_ICONINFORMATION))
	} else {
		log.Println(msg)
	}
}
