package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func HandleCLICommand(cmd string) {
	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		fmt.Println("🛡️ SnapHaven Server Command Line Interface")
		fmt.Println("--------------------------------------------------")
		fmt.Println("Usage: snaphaven-server [command]")
		fmt.Println("\nCommands:")
		fmt.Println("  open       Open the web dashboard and pairing QR code in default browser")
		fmt.Println("  status     Display active server status and setup URL")
		fmt.Println("  help       Show this help message")
		return
	}

	fmt.Println("🛡️ SnapHaven Server Status & Setup Launcher")
	fmt.Println("--------------------------------------------------")

	url := FindActiveSetupURL()
	if url == "" {
		fmt.Println("⚠️ Could not detect an active SnapHaven setup URL from system logs.")
		fmt.Println("Please verify the server service is running:")
		fmt.Println("  sudo systemctl status snaphaven-server")
		os.Exit(1)
	}

	fmt.Printf("🌐 Active Dashboard & QR Code URL: %s\n", url)

	if cmd == "open" || cmd == "dashboard" || cmd == "qr" || cmd == "status" || cmd == "--open" || cmd == "-open" {
		fmt.Println("📱 Opening Web Dashboard in desktop browser...")
		OpenBrowser(url)
	}
}

func FindActiveSetupURL() string {
	// 1. Try querying systemd journalctl logs (Linux)
	if runtime.GOOS == "linux" {
		out, err := exec.Command("journalctl", "-u", "snaphaven-server", "-n", "100", "--no-pager").Output()
		if err == nil {
			if u := extractURLFromLog(string(out)); u != "" {
				return u
			}
		}
	}

	// 2. Try reading default log file path
	configPath, err := GetDefaultConfigPath()
	if err == nil {
		logFilePath := filepath.Join(filepath.Dir(configPath), "snaphaven.log")
		if data, err := os.ReadFile(logFilePath); err == nil {
			if u := extractURLFromLog(string(data)); u != "" {
				return u
			}
		}
	}

	// 3. Fallback check for /tmp/snaphaven.log
	if data, err := os.ReadFile("/tmp/snaphaven.log"); err == nil {
		if u := extractURLFromLog(string(data)); u != "" {
			return u
		}
	}

	return ""
}

func extractURLFromLog(content string) string {
	re := regexp.MustCompile(`Setup Web Interface running at:\s*(http://[^\s]+)`)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) > 0 {
		lastMatch := matches[len(matches)-1]
		if len(lastMatch) > 1 {
			return strings.TrimSpace(lastMatch[1])
		}
	}
	return ""
}
