package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SetAutoStartConfig configures OS-native background auto-start on boot.
func SetAutoStartConfig(enabled bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get server executable path: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		// Windows auto-start handled by installer startup folder shortcut / registry
		log.Printf("ℹ️ Windows autostart configuration updated (enabled=%v)", enabled)
		return nil

	case "darwin":
		// macOS LaunchAgent plist: ~/Library/LaunchAgents/app.snaphaven.server.plist
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
		plistPath := filepath.Join(launchAgentsDir, "app.snaphaven.server.plist")

		if !enabled {
			os.Remove(plistPath)
			return nil
		}

		if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
			return err
		}

		var execArgs string
		if strings.Contains(exePath, ".app/Contents/MacOS/") {
			execArgs = fmt.Sprintf("<string>open</string>\n        <string>-a</string>\n        <string>%s</string>", exePath[:strings.Index(exePath, ".app/Contents/MacOS/")+4])
		} else {
			execArgs = fmt.Sprintf("<string>%s</string>", exePath)
		}

		plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>app.snaphaven.server</string>
    <key>ProgramArguments</key>
    <array>
        %s
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
</dict>
</plist>`, execArgs)

		return os.WriteFile(plistPath, []byte(plistContent), 0644)

	case "linux":
		// Linux XDG Autostart: ~/.config/autostart/snaphaven.desktop
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		autostartDir := filepath.Join(home, ".config", "autostart")
		desktopPath := filepath.Join(autostartDir, "snaphaven.desktop")

		if !enabled {
			os.Remove(desktopPath)
			return nil
		}

		if err := os.MkdirAll(autostartDir, 0755); err != nil {
			return err
		}

		desktopContent := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=SnapHaven Server
Comment=Secure mTLS Photo & Video Backup Server
Exec=%s
Icon=snaphaven
Terminal=false
Categories=Utility;
X-GNOME-Autostart-enabled=true
`, exePath)

		return os.WriteFile(desktopPath, []byte(desktopContent), 0644)

	default:
		return nil
	}
}
