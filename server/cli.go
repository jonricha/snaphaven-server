package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func HandleCLICommand(cmd string) {
	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		fmt.Println("🛡️ SnapHaven Server Command Line Interface")
		fmt.Println("--------------------------------------------------")
		fmt.Println("Usage: snaphaven-server [command]")
		fmt.Println("\nCommands:")
		fmt.Println("  qr         Render the mobile pairing QR code directly in the terminal")
		fmt.Println("  open       Open the web dashboard and pairing QR code in default browser")
		fmt.Println("  photos     Open the synced photos folder in the system file explorer")
		fmt.Println("  status     Display active server status and setup URL")
		fmt.Println("  help       Show this help message")
		return
	}

	if cmd == "photos" || cmd == "folder" || cmd == "--photos" || cmd == "--folder" {
		cfgMgr, err := NewConfigManager("")
		if err != nil {
			fmt.Printf("⚠️ Failed to load configuration: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("📁 Opening photos folder: %s\n", cfgMgr.Config.SyncDirectory)
		if err := OpenFolder(cfgMgr.Config.SyncDirectory); err != nil {
			fmt.Printf("⚠️ Could not open file explorer: %v\n", err)
			os.Exit(1)
		}
		return
	}

	url := FindActiveSetupURL()
	if url == "" {
		fmt.Println("⚠️ Could not detect an active SnapHaven setup URL from system logs or files.")
		fmt.Println("Please verify the server service is running:")
		fmt.Println("  sudo systemctl status snaphaven-server")
		os.Exit(1)
	}

	if cmd == "qr" || cmd == "--qr" {
		payload, fp := FindActivePairingPayload(url)
		if payload != "" {
			PrintTerminalQRCode(payload, url, fp)
			return
		}
		fmt.Printf("⚠️ Could not retrieve active pairing payload from %s.\n", url)
		fmt.Printf("You can view the QR code in your browser at: %s\n", url)
		return
	}

	fmt.Println("🛡️ SnapHaven Server Status & Setup Launcher")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("🌐 Active Dashboard & QR Code URL: %s\n", url)

	if cmd == "open" || cmd == "dashboard" || cmd == "--open" || cmd == "-open" {
		fmt.Println("📱 Opening Web Dashboard in desktop browser...")
		OpenBrowser(url)
	}
}

func GetActiveURLFilePath() string {
	configPath, err := GetDefaultConfigPath()
	if err == nil {
		return filepath.Join(filepath.Dir(configPath), "snaphaven.url")
	}
	return ""
}

func FindActivePairingPayload(activeURL string) (string, string) {
	// 1. Try reading persisted snaphaven.qr file
	if urlFile := GetActiveURLFilePath(); urlFile != "" {
		qrFile := filepath.Join(filepath.Dir(urlFile), "snaphaven.qr")
		if data, err := os.ReadFile(qrFile); err == nil && len(data) > 0 {
			var parsed struct {
				FP string `json:"fp"`
			}
			_ = json.Unmarshal(data, &parsed)
			return string(data), parsed.FP
		}
	}

	// 2. Query /api/pair/payload over HTTP from active server
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(activeURL + "/api/pair/payload")
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var res struct {
			Payload       string `json:"payload"`
			CAFingerprint string `json:"ca_fingerprint"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && res.Payload != "" {
			return res.Payload, res.CAFingerprint
		}
	}

	return "", ""
}

func FindActiveSetupURL() string {
	// 1. Check for persisted active URL file
	if urlFile := GetActiveURLFilePath(); urlFile != "" {
		if data, err := os.ReadFile(urlFile); err == nil {
			u := strings.TrimSpace(string(data))
			if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				return u
			}
		}
	}

	// 2. Try querying systemd journalctl logs (Linux)
	if runtime.GOOS == "linux" {
		out, err := exec.Command("journalctl", "-u", "snaphaven-server", "-n", "100", "--no-pager").Output()
		if err == nil {
			if u := extractURLFromLog(string(out)); u != "" {
				return u
			}
		}
	}

	// 3. Try reading default log file path
	configPath, err := GetDefaultConfigPath()
	if err == nil {
		logFilePath := filepath.Join(filepath.Dir(configPath), "snaphaven.log")
		if data, err := os.ReadFile(logFilePath); err == nil {
			if u := extractURLFromLog(string(data)); u != "" {
				return u
			}
		}
	}

	// 4. Fallback check for /tmp/snaphaven.log
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
