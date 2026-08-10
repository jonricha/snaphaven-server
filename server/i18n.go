package main

import (
	"os"
	"strings"
)

// TrayStrings holds localized titles and tooltips for the system tray menu.
type TrayStrings struct {
	StatusRunning   string
	StatusStopped   string
	ShowQRCode      string
	ShowQRTooltip   string
	OpenDashboard   string
	DashTooltip     string
	Settings        string
	SettingsTooltip string
	ViewLogs        string
	LogsTooltip     string
	CheckUpdates    string
	UpdatesTooltip  string
	InstallUpdate   string
	About           string
	AboutTooltip    string
	PauseServer     string
	StartServer     string
	Quit            string
	QuitTooltip     string
}

var englishTrayStrings = TrayStrings{
	StatusRunning:   "Status: Server Running",
	StatusStopped:   "Status: Server Stopped",
	ShowQRCode:      "📱 Show Pairing QR Code",
	ShowQRTooltip:   "Open pairing page in browser",
	OpenDashboard:   "🌐 Open Web Dashboard",
	DashTooltip:     "Open management dashboard in browser",
	Settings:        "⚙️ Settings",
	SettingsTooltip: "Open settings tab in browser",
	ViewLogs:        "📋 View Live Logs",
	LogsTooltip:     "Open logs tab in browser",
	CheckUpdates:    "🔄 Check for Updates...",
	UpdatesTooltip:  "Check for software updates",
	InstallUpdate:   "✨ Install Update (%s)...",
	About:           "ℹ️ About SnapHaven...",
	AboutTooltip:    "View software version, author, and build details",
	PauseServer:     "Pause Server",
	StartServer:     "Start Server",
	Quit:            "Quit SnapHaven",
	QuitTooltip:     "Quit the SnapHaven server application",
}

// DetectLanguage inspects environment variables to determine system language code (e.g. "en", "es", "de").
func DetectLanguage() string {
	langEnv := os.Getenv("LANG")
	if langEnv == "" {
		langEnv = os.Getenv("LC_ALL")
	}
	if langEnv == "" {
		langEnv = os.Getenv("LANGUAGE")
	}
	langEnv = strings.ToLower(langEnv)
	if strings.HasPrefix(langEnv, "es") {
		return "es"
	}
	if strings.HasPrefix(langEnv, "de") {
		return "de"
	}
	if strings.HasPrefix(langEnv, "fr") {
		return "fr"
	}
	return "en"
}

// GetTrayStrings returns the localized TrayStrings dictionary for the detected or configured language.
func GetTrayStrings() TrayStrings {
	// Defaults to English; future language maps (e.g., spanishTrayStrings) can be matched here seamlessly.
	switch DetectLanguage() {
	default:
		return englishTrayStrings
	}
}
