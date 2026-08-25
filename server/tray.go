package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"

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
	ts := GetTrayStrings()
	systray.SetTitle("SnapHaven")
	systray.SetTooltip("SnapHaven Server " + GetFormattedVersion())

	// Set system tray icon from embedded icon.ico
	systray.SetIcon(trayIconBytes)

	mStatus := systray.AddMenuItem(ts.StatusRunning, "Server Status")
	mStatus.Disable()

	mQR := systray.AddMenuItem(ts.ShowQRCode, ts.ShowQRTooltip)
	mDashboard := systray.AddMenuItem(ts.OpenDashboard, ts.DashTooltip)
	mSettings := systray.AddMenuItem(ts.Settings, ts.SettingsTooltip)
	mLogs := systray.AddMenuItem(ts.ViewLogs, ts.LogsTooltip)
	mCheckUpdate := systray.AddMenuItem(ts.CheckUpdates, ts.UpdatesTooltip)
	mAbout := systray.AddMenuItem(ts.About, ts.AboutTooltip)

	systray.AddSeparator()
	mToggle := systray.AddMenuItem(ts.PauseServer, "Toggle gRPC sync server")
	mQuit := systray.AddMenuItem(ts.Quit, ts.QuitTooltip)

	if t.updater != nil {
		t.updater.SetUpdateCallback(func(info *UpdateInfo) {
			mCheckUpdate.SetTitle(fmt.Sprintf(ts.InstallUpdate, info.Version))
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
			mStatus.SetTitle(ts.StatusStopped)
			mToggle.SetTitle(ts.StartServer)
		} else {
			if err := t.serverMgr.Start(); err == nil {
				mStatus.SetTitle(ts.StatusRunning)
				mToggle.SetTitle(ts.PauseServer)
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
