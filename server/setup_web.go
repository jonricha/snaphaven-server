package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

type PairingPayload struct {
	IP            string `json:"ip"`
	Port          int    `json:"port"`
	Token         string `json:"token"`
	CAFingerprint string `json:"ca_fingerprint"`
}

type PairRequest struct {
	Token string `json:"token"`
	CSR   string `json:"csr"`
}

type PairResponse struct {
	Success     bool   `json:"success"`
	Certificate string `json:"certificate,omitempty"`
	CACert      string `json:"ca_cert,omitempty"`
	Error       string `json:"error,omitempty"`
}

func GenerateToken() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GetLocalIPs() []string {
	var homeLANIPs []string
	var physicalIPs []string
	var fallbackIPs []string

	ifaces, err := net.Interfaces()
	if err != nil {
		return physicalIPs
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		nameLower := strings.ToLower(iface.Name)
		isVirtualOrVPN := iface.Flags&net.FlagPointToPoint != 0 ||
			strings.Contains(nameLower, "vpn") ||
			strings.Contains(nameLower, "tun") ||
			strings.Contains(nameLower, "tap") ||
			strings.Contains(nameLower, "wireguard") ||
			strings.Contains(nameLower, "wg") ||
			strings.Contains(nameLower, "tailscale") ||
			strings.Contains(nameLower, "zerotier") ||
			strings.Contains(nameLower, "cisco") ||
			strings.Contains(nameLower, "globalprotect") ||
			strings.Contains(nameLower, "vbox") ||
			strings.Contains(nameLower, "vmnet") ||
			strings.Contains(nameLower, "hyper-v") ||
			strings.Contains(nameLower, "vethernet") ||
			strings.Contains(nameLower, "wsl") ||
			strings.Contains(nameLower, "virtual") ||
			strings.Contains(nameLower, "pseudo") ||
			strings.Contains(nameLower, "bridge") ||
			strings.Contains(nameLower, "forti") ||
			strings.Contains(nameLower, "pulse") ||
			strings.Contains(nameLower, "secu") ||
			strings.Contains(nameLower, "openvpn")

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ipStr := ipnet.IP.String()
					if isVirtualOrVPN {
						fallbackIPs = append(fallbackIPs, ipStr)
					} else if strings.HasPrefix(ipStr, "192.168.") {
						homeLANIPs = append(homeLANIPs, ipStr)
					} else {
						physicalIPs = append(physicalIPs, ipStr)
					}
				}
			}
		}
	}

	result := append(homeLANIPs, physicalIPs...)
	return append(result, fallbackIPs...)
}

func GetPrimarySubnet() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var fallbackSubnet string

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		nameLower := strings.ToLower(iface.Name)
		isVirtualOrVPN := iface.Flags&net.FlagPointToPoint != 0 ||
			strings.Contains(nameLower, "vpn") ||
			strings.Contains(nameLower, "tun") ||
			strings.Contains(nameLower, "tap") ||
			strings.Contains(nameLower, "wireguard") ||
			strings.Contains(nameLower, "wg") ||
			strings.Contains(nameLower, "tailscale") ||
			strings.Contains(nameLower, "zerotier") ||
			strings.Contains(nameLower, "cisco") ||
			strings.Contains(nameLower, "globalprotect") ||
			strings.Contains(nameLower, "vbox") ||
			strings.Contains(nameLower, "vmnet") ||
			strings.Contains(nameLower, "hyper-v") ||
			strings.Contains(nameLower, "wsl")

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					mask := ipnet.Mask
					netIP := ipnet.IP.Mask(mask)
					ones, _ := mask.Size()
					cidr := fmt.Sprintf("%s/%d", netIP.String(), ones)

					if !isVirtualOrVPN {
						return cidr
					} else if fallbackSubnet == "" {
						fallbackSubnet = cidr
					}
				}
			}
		}
	}

	return fallbackSubnet
}

type SetupServer struct {
	PairingToken  string
	GRPCAddress   string
	CAFingerprint string
	CertManager   *CertManager
	ConfigManager *ConfigManager
	ServerManager *ServerManager
	UpdateManager *UpdateManager
	HTTPListener  net.Listener
	ServerURL     string

	mu            sync.Mutex
	pairedDevices map[string]bool
}

func NewSetupServer(grpcAddr string, cm *CertManager, cfgMgr *ConfigManager, srvMgr *ServerManager, um *UpdateManager) (*SetupServer, error) {
	token, err := GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate pairing token: %w", err)
	}

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start web setup listener: %w", err)
	}

	s := &SetupServer{
		PairingToken:  token,
		GRPCAddress:   grpcAddr,
		CAFingerprint: cm.CAFingerprint,
		CertManager:   cm,
		ConfigManager: cfgMgr,
		ServerManager: srvMgr,
		UpdateManager: um,
		HTTPListener:  listener,
		pairedDevices: make(map[string]bool),
	}

	port := listener.Addr().(*net.TCPAddr).Port
	localIPs := GetLocalIPs()
	hostIP := "localhost"
	if len(localIPs) > 0 {
		hostIP = localIPs[0]
	}
	s.ServerURL = fmt.Sprintf("http://%s:%d", hostIP, port)

	return s, nil
}

const dashboardHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SnapHaven Dashboard</title>
    <script src="https://cdn.jsdelivr.net/npm/qrcodejs@1.0.0/qrcode.min.js"></script>
    <style>
        :root {
            --bg-primary: #0f172a;
            --bg-card: #1e293b;
            --border-color: #334155;
            --accent-color: #0284c7;
            --accent-hover: #0369a1;
            --text-main: #f8fafc;
            --text-muted: #94a3b8;
            --success: #10b981;
            --danger: #ef4444;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            background: var(--bg-primary);
            color: var(--text-main);
            margin: 0;
            padding: 0;
            box-sizing: border-box;
            min-height: 100vh;
        }

        .navbar {
            background: var(--bg-card);
            border-bottom: 1px solid var(--border-color);
            padding: 16px 24px;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }

        .brand {
            font-size: 1.25rem;
            font-weight: bold;
            color: #38bdf8;
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .nav-links {
            display: flex;
            gap: 12px;
        }

        .nav-btn {
            background: transparent;
            border: 1px solid transparent;
            color: var(--text-muted);
            padding: 8px 16px;
            border-radius: 8px;
            cursor: pointer;
            font-weight: 500;
            transition: all 0.2s;
        }

        .nav-btn:hover {
            color: var(--text-main);
            background: rgba(255,255,255,0.05);
        }

        .nav-btn.active {
            color: white;
            background: var(--accent-color);
        }

        .container {
            max-width: 900px;
            margin: 32px auto;
            padding: 0 20px;
        }

        .card {
            background: var(--bg-card);
            border-radius: 16px;
            padding: 28px;
            box-shadow: 0 10px 25px rgba(0,0,0,0.4);
            border: 1px solid var(--border-color);
            margin-bottom: 24px;
        }

        .tab-content {
            display: none;
        }

        .tab-content.active {
            display: block;
        }

        .status-badge {
            display: inline-flex;
            align-items: center;
            gap: 6px;
            font-size: 0.85rem;
            padding: 6px 12px;
            border-radius: 9999px;
            font-weight: 600;
        }

        .status-running {
            background: rgba(16, 185, 129, 0.15);
            color: var(--success);
            border: 1px solid var(--success);
        }

        .status-stopped {
            background: rgba(239, 68, 68, 0.15);
            color: var(--danger);
            border: 1px solid var(--danger);
        }

        #qrcode {
            background: #ffffff;
            padding: 16px;
            border-radius: 12px;
            display: inline-block;
            margin: 20px 0;
        }

        .info-box {
            background: var(--bg-primary);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 14px;
            font-family: monospace;
            font-size: 0.85rem;
            word-break: break-all;
            margin-top: 16px;
        }

        .banner-notice {
            background: rgba(2, 132, 199, 0.15);
            border: 1px solid var(--accent-color);
            border-radius: 12px;
            padding: 16px;
            margin-bottom: 24px;
            display: flex;
            align-items: center;
            gap: 12px;
        }

        .form-group {
            margin-bottom: 20px;
        }

        label {
            display: block;
            margin-bottom: 8px;
            font-weight: 500;
            color: var(--text-muted);
        }

        input[type="text"] {
            width: 100%;
            padding: 10px 14px;
            background: var(--bg-primary);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            color: white;
            box-sizing: border-box;
            font-size: 0.95rem;
        }

        .btn {
            background: var(--accent-color);
            color: white;
            border: none;
            padding: 10px 20px;
            border-radius: 8px;
            font-weight: bold;
            cursor: pointer;
            transition: background 0.2s;
        }

        .btn:hover {
            background: var(--accent-hover);
        }

        .btn-danger {
            background: var(--danger);
        }

        .btn-danger:hover {
            background: #dc2626;
        }

        #logViewer {
            background: #090d16;
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 16px;
            font-family: 'Courier New', Courier, monospace;
            font-size: 0.85rem;
            height: 400px;
            overflow-y: auto;
            white-space: pre-wrap;
            color: #38bdf8;
        }

        .modal-overlay {
            position: fixed;
            top: 0; left: 0; right: 0; bottom: 0;
            background: rgba(0, 0, 0, 0.75);
            backdrop-filter: blur(4px);
            display: flex;
            align-items: center;
            justify-content: center;
            z-index: 1000;
        }

        .modal-card {
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            padding: 24px;
            max-width: 480px;
            width: 90%;
            box-shadow: 0 20px 40px rgba(0,0,0,0.6);
        }

        .release-notes-box {
            background: var(--bg-primary);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 12px;
            font-size: 0.85rem;
            max-height: 160px;
            overflow-y: auto;
            white-space: pre-wrap;
            margin: 12px 0;
            color: var(--text-muted);
        }

        .progress-bar-bg {
            background: var(--border-color);
            border-radius: 9999px;
            height: 10px;
            overflow: hidden;
            width: 100%;
        }

        .progress-bar-fill {
            background: var(--accent-color);
            height: 100%;
            width: 0%;
            transition: width 0.3s ease;
        }

        .version-badge {
            font-size: 0.75rem;
            background: rgba(56, 189, 248, 0.15);
            color: #38bdf8;
            border: 1px solid rgba(56, 189, 248, 0.3);
            padding: 2px 8px;
            border-radius: 12px;
            font-weight: normal;
        }
    </style>
</head>
<body>

    <div class="navbar">
        <div class="brand">
            🛡️ SnapHaven Server <span class="version-badge" id="navVersionBadge">v...</span>
        </div>
        <div class="nav-links">
            <button id="btn-pairing" class="nav-btn active" onclick="showTab('pairing')">📱 Pairing QR Code</button>
            <button id="btn-status" class="nav-btn" onclick="showTab('status')">📊 Server Status</button>
            <button id="btn-settings" class="nav-btn" onclick="showTab('settings')">⚙️ Settings</button>
            <button id="btn-logs" class="nav-btn" onclick="showTab('logs')">📋 Live Logs</button>
            <button id="btn-about" class="nav-btn" onclick="showTab('about')">ℹ️ About</button>
        </div>
    </div>

    <div class="container">
        {{if .IsFirstRun}}
        <div class="banner-notice">
            <div style="font-size: 1.5rem;">💡</div>
            <div>
                <strong>Welcome to SnapHaven!</strong><br>
                SnapHaven is now running in your <strong>system tray</strong>. You can right-click the tray icon anytime to view status, pause the server, or open this dashboard.
            </div>
        </div>
        {{end}}

        <!-- Pairing Tab -->
        <div id="pairing" class="tab-content active">
            <div class="card" style="text-align: center;">
                <h2>Pair Mobile Device</h2>
                <p style="color: var(--text-muted);">Scan this QR code with the <strong>SnapHaven Android App</strong> to pair your device with mTLS security.</p>

                <div id="qrcode"></div>

                <div class="info-box" style="text-align: left;">
                    <div><strong>gRPC Target:</strong> {{.IP}}:{{.Port}}</div>
                    <div style="margin-top: 6px;"><strong>CA Fingerprint:</strong> {{.CAFingerprint}}</div>
                </div>

                <div style="margin-top: 20px;">
                    <button id="fwBtn" class="btn">🛡️ Allow Windows Firewall Access</button>
                    <div id="fwStatus" style="font-size: 0.8rem; margin-top: 8px; color: var(--text-muted);"></div>
                </div>
            </div>
        </div>

        <!-- Status Tab -->
        <div id="status" class="tab-content">
            <div class="card">
                <h2>Server Control & Status</h2>
                <div style="display: flex; align-items: center; justify-content: space-between; margin: 20px 0;">
                    <div>
                        <div style="font-size: 0.9rem; color: var(--text-muted);">gRPC Service Status</div>
                        <div id="statusBadgeContainer" style="margin-top: 6px;">
                            <span class="status-badge status-running">🟢 Server Running</span>
                        </div>
                    </div>
                    <div>
                        <button id="toggleServerBtn" class="btn btn-danger" onclick="toggleServer()">Stop Server</button>
                    </div>
                </div>
                <hr style="border: 0; border-top: 1px solid var(--border-color); margin: 20px 0;">
                <div class="info-box">
                    <div><strong>Active Sync Directory:</strong> <span id="currentSyncDir">{{.Config.SyncDirectory}}</span></div>
                    <div style="margin-top: 6px;"><strong>gRPC Port:</strong> <span id="currentPort">{{.Config.GRPCPort}}</span></div>
                </div>
            </div>
        </div>

        <!-- Settings Tab -->
        <div id="settings" class="tab-content">
            <div class="card">
                <h2>Server Preferences</h2>
                <form id="settingsForm" onsubmit="saveSettings(event)">
                    <div class="form-group">
                        <label for="syncDir">Sync Storage Folder Path</label>
                        <input type="text" id="syncDir" value="{{.Config.SyncDirectory}}">
                    </div>
                    <div class="form-group">
                        <label for="grpcPort">gRPC Listen Port</label>
                        <input type="text" id="grpcPort" value="{{.Config.GRPCPort}}">
                    </div>
                    <div class="form-group" style="display: flex; align-items: center; gap: 10px;">
                        <input type="checkbox" id="autoStart" {{if .Config.AutoStartOnBoot}}checked{{end}}>
                        <label for="autoStart" style="margin: 0;">Launch SnapHaven automatically on system startup</label>
                    </div>
                    <button type="submit" class="btn">💾 Save & Restart Server</button>
                    <div id="settingsStatus" style="margin-top: 10px; font-size: 0.85rem;"></div>
                </form>
            </div>

            <div class="card">
                <h2>Software Updates & Version</h2>
                <div style="display: flex; align-items: center; justify-content: space-between; margin-top: 12px;">
                    <div>
                        <div style="font-size: 0.9rem; color: var(--text-muted);">Installed Server Version</div>
                        <div style="font-size: 1.1rem; font-weight: bold; margin-top: 4px;" id="settingsVersionText">Loading...</div>
                        <div id="updateStatusSubtext" style="font-size: 0.85rem; color: var(--text-muted); margin-top: 4px;"></div>
                    </div>
                    <button type="button" class="btn" id="checkUpdatesBtn" onclick="checkUpdates(true)">🔄 Check for Updates</button>
                </div>
            </div>
        </div>

        <!-- Logs Tab -->
        <div id="logs" class="tab-content">
            <div class="card">
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px;">
                    <h2 style="margin: 0;">Live System Logs</h2>
                    <div style="display: flex; gap: 8px;">
                        <button class="btn" style="padding: 6px 12px; font-size: 0.8rem;" onclick="copyLogs()">📋 Copy Logs</button>
                        <button class="btn" style="padding: 6px 12px; font-size: 0.8rem;" onclick="clearLogViewer()">Clear View</button>
                    </div>
                </div>
                <div id="logViewer">Listening for live logs...</div>
                <div id="copyStatus" style="font-size: 0.8rem; color: var(--text-muted); margin-top: 8px;"></div>
            </div>
        </div>

        <!-- About Tab -->
        <div id="about" class="tab-content">
            <div class="card">
                <div style="display: flex; align-items: center; gap: 16px; margin-bottom: 20px;">
                    <div style="font-size: 2.5rem;">🛡️</div>
                    <div>
                        <h2 style="margin: 0; color: #38bdf8;">SnapHaven Server</h2>
                        <div style="color: var(--text-muted); font-size: 0.9rem;">Secure mTLS Photo & File Synchronization Engine</div>
                    </div>
                </div>

                <div class="info-box" style="margin-bottom: 20px;">
                    <div><strong>Version:</strong> <span id="aboutVersionStr">Loading...</span></div>
                    <div style="margin-top: 6px;"><strong>Git Commit:</strong> <span id="aboutCommitStr">Loading...</span></div>
                    <div style="margin-top: 6px;"><strong>Build Timestamp:</strong> <span id="aboutBuildTimeStr">Loading...</span></div>
                    <div style="margin-top: 6px;"><strong>Author:</strong> Jonathan Richardson</div>
                    <div style="margin-top: 6px;"><strong>License:</strong> MIT License</div>
                    <div style="margin-top: 6px;"><strong>GitHub Repository:</strong> <a href="https://github.com/jonricha/snaphaven-server" target="_blank" style="color: #38bdf8; text-decoration: none;">https://github.com/jonricha/snaphaven-server</a></div>
                </div>

                <div style="display: flex; gap: 12px;">
                    <button class="btn" onclick="checkUpdates(true)">🔄 Check for Updates</button>
                    <a href="https://github.com/jonricha/snaphaven-server" target="_blank" class="btn" style="background: transparent; border: 1px solid var(--border-color); text-decoration: none; display: inline-block;">🌐 View Source on GitHub</a>
                </div>
            </div>
        </div>

    </div>

    <!-- Update Prompt Modal -->
    <div id="updateModal" class="modal-overlay" style="display: none;">
        <div class="modal-card">
            <h3 id="updateModalTitle" style="margin-top: 0;">✨ SnapHaven Update Available!</h3>
            <div id="updateModalVersion" style="font-weight: bold; color: #38bdf8; font-size: 1rem; margin-bottom: 8px;"></div>
            <div id="updateModalNotes" class="release-notes-box">Checking for release notes...</div>
            <div id="updateProgressContainer" style="display: none; margin-top: 16px;">
                <div class="progress-bar-bg"><div id="updateProgressBar" class="progress-bar-fill"></div></div>
                <div id="updateProgressText" style="font-size: 0.8rem; color: var(--text-muted); margin-top: 6px; text-align: center;">Downloading update package...</div>
            </div>
            <div id="updateModalActions" class="modal-actions" style="margin-top: 20px; display: flex; gap: 10px; justify-content: flex-end;">
                <button id="updateLaterBtn" class="btn" style="background: transparent; border: 1px solid var(--border-color); color: var(--text-muted);" onclick="closeUpdateModal()">Later</button>
                <button id="updateNowBtn" class="btn" onclick="performUpdate()">🚀 Update Now</button>
            </div>
        </div>
    </div>

    <script>
        let currentUpdateInfo = null;

        function fetchVersionInfo() {
            fetch("/api/version")
                .then(r => r.json())
                .then(data => {
                    const ver = data.formatted || data.version || "v0.0.0";
                    document.getElementById("navVersionBadge").innerText = ver;
                    document.getElementById("settingsVersionText").innerText = ver;
                    if (document.getElementById("aboutVersionStr")) document.getElementById("aboutVersionStr").innerText = data.version || "v0.0.0-dev";
                    if (document.getElementById("aboutCommitStr")) document.getElementById("aboutCommitStr").innerText = data.commit || "none";
                    if (document.getElementById("aboutBuildTimeStr")) document.getElementById("aboutBuildTimeStr").innerText = data.build_time || "unknown";

                    if (data.update_status) {
                        const st = data.update_status;
                        const sub = document.getElementById("updateStatusSubtext");

                        if (st.state === "update_available") {
                            currentUpdateInfo = st.update_info;
                            sub.innerText = "✨ New version available: " + st.latest_version;
                            sub.style.color = "#38bdf8";

                            if (!sessionStorage.getItem("update_prompted")) {
                                showUpdateModal(st.update_info);
                            }
                        } else if (st.state === "no_update") {
                            sub.innerText = "✅ Server is running the latest version.";
                            sub.style.color = "var(--success)";
                        } else if (st.state === "downloading") {
                            sub.innerText = "📥 Downloading update... (" + (st.progress || 0) + "%)";
                        } else if (st.state === "ready_to_install") {
                            sub.innerText = "Ready to install update (" + st.latest_version + ")";
                        }
                    }
                })
                .catch(err => console.error("Error fetching version:", err));
        }

        function checkUpdates(manual = false) {
            const btn = document.getElementById("checkUpdatesBtn");
            if (btn) btn.innerText = "Checking...";
            const sub = document.getElementById("updateStatusSubtext");
            if (sub) sub.innerText = "Querying latest release from GitHub...";

            fetch("/api/check-update", { method: "POST" })
                .then(r => r.json())
                .then(data => {
                    if (btn) btn.innerText = "🔄 Check for Updates";
                    if (data.has_update && data.info) {
                        showUpdateModal(data.info);
                    } else if (manual) {
                        alert("You are running the latest version of SnapHaven Server (" + (data.current_version || "up-to-date") + ").");
                    }
                    fetchVersionInfo();
                })
                .catch(err => {
                    if (btn) btn.innerText = "🔄 Check for Updates";
                    if (manual) alert("Failed to check for updates: " + err);
                });
        }

        function showUpdateModal(info) {
            currentUpdateInfo = info;
            sessionStorage.setItem("update_prompted", "true");
            document.getElementById("updateModalVersion").innerText = "Version " + (info ? info.version : "");
            document.getElementById("updateModalNotes").innerText = (info && info.release_notes) ? info.release_notes : "No release notes available.";
            document.getElementById("updateModal").style.display = "flex";
            document.getElementById("updateProgressContainer").style.display = "none";
            document.getElementById("updateModalActions").style.display = "flex";
        }

        function closeUpdateModal() {
            document.getElementById("updateModal").style.display = "none";
        }

        function performUpdate() {
            document.getElementById("updateModalActions").style.display = "none";
            document.getElementById("updateProgressContainer").style.display = "block";
            document.getElementById("updateProgressText").innerText = "Starting update download...";

            fetch("/api/perform-update", { method: "POST" })
                .then(r => r.json())
                .then(data => {
                    if (!data.success) {
                        alert("Update failed: " + (data.error || "Unknown error"));
                        closeUpdateModal();
                        return;
                    }
                    pollUpdateProgress();
                })
                .catch(err => {
                    alert("Error initiating update: " + err);
                    closeUpdateModal();
                });
        }

        function pollUpdateProgress() {
            const interval = setInterval(() => {
                fetch("/api/version")
                    .then(r => r.json())
                    .then(data => {
                        const st = data.update_status;
                        if (!st) return;

                        if (st.state === "downloading") {
                            const prog = st.progress || 0;
                            document.getElementById("updateProgressBar").style.width = prog + "%";
                            document.getElementById("updateProgressText").innerText = "Downloading update package... (" + prog + "%)";
                        } else if (st.state === "ready_to_install") {
                            clearInterval(interval);
                            document.getElementById("updateProgressBar").style.width = "100%";
                            document.getElementById("updateProgressText").innerText = "Update ready! Installer launched. Server is restarting...";
                            setTimeout(() => {
                                closeUpdateModal();
                                location.reload();
                            }, 5000);
                        } else if (st.state === "error") {
                            clearInterval(interval);
                            alert("Update failed: " + st.error_message);
                            closeUpdateModal();
                        }
                    });
            }, 1000);
        }

        const pairingData = {{.JSONData}};
        new QRCode(document.getElementById("qrcode"), {
            text: JSON.stringify(pairingData),
            width: 220,
            height: 220,
            colorDark : "#0f172a",
            colorLight : "#ffffff",
            correctLevel : QRCode.CorrectLevel.M
        });

        function showTab(tabId) {
            const targetTab = document.getElementById(tabId);
            if (!targetTab) return;
            document.querySelectorAll('.tab-content').forEach(el => el.classList.remove('active'));
            document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('active'));
            targetTab.classList.add('active');
            const navBtn = document.getElementById('btn-' + tabId);
            if (navBtn) navBtn.classList.add('active');
            window.location.hash = tabId;
        }

        function handleHashRouting() {
            let hash = window.location.hash.replace('#', '');
            if (!hash || !document.getElementById(hash)) {
                hash = 'pairing';
            }
            showTab(hash);
        }

        window.addEventListener('load', handleHashRouting);
        window.addEventListener('hashchange', handleHashRouting);

        document.getElementById("fwBtn").addEventListener("click", () => {
            document.getElementById("fwStatus").innerText = "Checking firewall rules...";
            fetch("/api/firewall", { method: "POST" })
                .then(r => r.json())
                .then(data => {
                    if (data.message) {
                        document.getElementById("fwStatus").innerText = data.message;
                    } else if (data.success) {
                        document.getElementById("fwStatus").innerText = "✅ Firewall configuration triggered!";
                    } else {
                        document.getElementById("fwStatus").innerText = "❌ " + (data.error || "Failed");
                    }
                });
        });

        function updateStatusUI() {
            fetch("/api/status")
                .then(r => r.json())
                .then(data => {
                    const badgeContainer = document.getElementById("statusBadgeContainer");
                    const toggleBtn = document.getElementById("toggleServerBtn");
                    if (data.running) {
                        badgeContainer.innerHTML = '<span class="status-badge status-running">🟢 Server Running</span>';
                        toggleBtn.innerText = "Stop Server";
                        toggleBtn.className = "btn btn-danger";
                    } else {
                        badgeContainer.innerHTML = '<span class="status-badge status-stopped">🔴 Server Stopped</span>';
                        toggleBtn.innerText = "Start Server";
                        toggleBtn.className = "btn";
                    }
                    document.getElementById("currentSyncDir").innerText = data.sync_directory;
                    document.getElementById("currentPort").innerText = data.grpc_port;
                });
        }
        setInterval(updateStatusUI, 3000);
        updateStatusUI();

        function toggleServer() {
            fetch("/api/server/toggle", { method: "POST" })
                .then(r => r.json())
                .then(() => updateStatusUI());
        }

        function saveSettings(e) {
            e.preventDefault();
            const statusEl = document.getElementById("settingsStatus");
            statusEl.innerText = "Saving settings...";
            fetch("/api/config", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    sync_directory: document.getElementById("syncDir").value,
                    grpc_port: document.getElementById("grpcPort").value,
                    auto_start_on_boot: document.getElementById("autoStart").checked
                })
            })
            .then(r => r.json())
            .then(data => {
                if (data.success) {
                    statusEl.innerText = "✅ Settings saved successfully!";
                    updateStatusUI();
                } else {
                    statusEl.innerText = "❌ Error: " + data.error;
                }
            });
        }

        // Live Log Buffer & Polling Handler
        const logViewer = document.getElementById("logViewer");
        let firstLog = true;
        let logLines = [];
        const MAX_LOG_LINES = 1000;

        function renderLogs() {
            logViewer.innerText = logLines.join("\n");
            logViewer.scrollTop = logViewer.scrollHeight;
        }

        function appendLog(timestamp, message) {
            let msg = message.trim();
            logLines.push("[" + timestamp + "] " + msg);
            if (logLines.length > MAX_LOG_LINES) {
                logLines = logLines.slice(-MAX_LOG_LINES);
            }
            renderLogs();
        }

        function fetchRecentLogs() {
            fetch("/api/logs/history")
                .then(r => r.json())
                .then(logs => {
                    if (Array.isArray(logs) && logs.length > 0) {
                        logLines = logs.map(item => "[" + item.timestamp + "] " + item.message.trim());
                        if (logLines.length > MAX_LOG_LINES) {
                            logLines = logLines.slice(-MAX_LOG_LINES);
                        }
                        renderLogs();
                    }
                })
                .catch(err => console.log("Log history fetch error:", err));
        }

        fetchRecentLogs();
        setInterval(fetchRecentLogs, 3000);

        function copyLogs() {
            const text = logViewer.innerText;
            navigator.clipboard.writeText(text).then(() => {
                const status = document.getElementById("copyStatus");
                status.innerText = "✅ Logs copied to clipboard!";
                setTimeout(() => { status.innerText = ""; }, 3000);
            }).catch(err => {
                alert("Failed to copy logs: " + err);
            });
        }

        function clearLogViewer() {
            logLines = [];
            logViewer.innerText = "";
        }
    </script>
</body>
</html>`

func (s *SetupServer) Start() {
	mux := http.NewServeMux()

	infoHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		hostname, _ := os.Hostname()
		parts := strings.Split(s.ConfigManager.Config.GRPCPort, ":")
		portNum := 50005
		if len(parts) > 1 {
			fmt.Sscanf(parts[len(parts)-1], "%d", &portNum)
		}
		httpPort := s.HTTPListener.Addr().(*net.TCPAddr).Port

		json.NewEncoder(w).Encode(map[string]interface{}{
			"server_id":   s.ConfigManager.Config.ServerID,
			"server_name": hostname,
			"version":     GetVersion(),
			"port":        portNum,
			"setup_port":  httpPort,
			"subnet":      GetPrimarySubnet(),
		})
	}
	mux.HandleFunc("/api/v1/info", infoHandler)
	mux.HandleFunc("/api/info", infoHandler)

	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var status UpdateStatus
		if s.UpdateManager != nil {
			status = s.UpdateManager.GetStatus()
		} else {
			status = UpdateStatus{State: StateIdle, CurrentVer: GetVersion()}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version":       GetVersion(),
			"commit":        GetCommit(),
			"build_time":    GetBuildTime(),
			"formatted":     GetFormattedVersion(),
			"update_status": status,
		})
	})

	mux.HandleFunc("/api/check-update", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if s.UpdateManager == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Update manager unavailable"})
			return
		}
		info, hasUpdate, err := s.UpdateManager.CheckForUpdates()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"has_update":      hasUpdate,
			"info":            info,
			"current_version": GetVersion(),
		})
	})

	mux.HandleFunc("/api/perform-update", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if s.UpdateManager == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Update manager unavailable"})
			return
		}
		go func() {
			err := s.UpdateManager.DownloadUpdate()
			if err != nil {
				return
			}
			s.UpdateManager.ApplyUpdate()
		}()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Update download and installation started.",
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		localIPs := GetLocalIPs()
		ip := "127.0.0.1"
		if len(localIPs) > 0 {
			ip = localIPs[0]
		}

		parts := strings.Split(s.ConfigManager.Config.GRPCPort, ":")
		portNum := 50005
		if len(parts) > 1 {
			fmt.Sscanf(parts[len(parts)-1], "%d", &portNum)
		}

		httpPort := s.HTTPListener.Addr().(*net.TCPAddr).Port

		payload := struct {
			IP            string   `json:"ip"`
			IPs           []string `json:"ips"`
			Port          int      `json:"port"`
			SetupPort     int      `json:"sport"`
			Token         string   `json:"token"`
			CAFingerprint string   `json:"fp"`
			ServerID      string   `json:"server_id"`
			Subnet        string   `json:"subnet"`
		}{
			IP:            ip,
			IPs:           localIPs,
			Port:          portNum,
			SetupPort:     httpPort,
			Token:         s.PairingToken,
			CAFingerprint: s.CAFingerprint,
			ServerID:      s.ConfigManager.Config.ServerID,
			Subnet:        GetPrimarySubnet(),
		}

		jsonData, _ := json.Marshal(payload)

		tmpl, err := template.New("dashboard").Parse(dashboardHTMLTemplate)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		data := struct {
			IP            string
			Port          int
			CAFingerprint string
			JSONData      string
			Config        Config
			IsFirstRun    bool
		}{
			IP:            ip,
			Port:          portNum,
			CAFingerprint: s.CAFingerprint,
			JSONData:      string(jsonData),
			Config:        s.ConfigManager.Config,
			IsFirstRun:    s.ConfigManager.IsFirstRun(),
		}

		w.Header().Set("Content-Type", "text/html")
		tmpl.Execute(w, data)
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"running":        s.ServerManager.IsRunning(),
			"sync_directory": s.ConfigManager.Config.SyncDirectory,
			"grpc_port":      s.ConfigManager.Config.GRPCPort,
		})
	})

	mux.HandleFunc("/api/server/toggle", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if s.ServerManager.IsRunning() {
			s.ServerManager.Stop()
		} else {
			s.ServerManager.Start()
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"running": s.ServerManager.IsRunning()})
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var newCfg Config
			if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
				json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
				return
			}
			newCfg.CertDirectory = s.ConfigManager.Config.CertDirectory
			if err := s.ConfigManager.Update(newCfg); err != nil {
				json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
				return
			}
			s.ServerManager.Restart()
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}
		json.NewEncoder(w).Encode(s.ConfigManager.Config)
	})

	mux.HandleFunc("/api/logs/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if globalLogHub == nil {
			json.NewEncoder(w).Encode([]LogEntry{})
			return
		}
		json.NewEncoder(w).Encode(globalLogHub.GetRecentLogs())
	})

	mux.HandleFunc("/api/logs/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		if globalLogHub == nil {
			return
		}

		ch := globalLogHub.Subscribe()
		defer globalLogHub.Unsubscribe(ch)

		recent := globalLogHub.GetRecentLogs()
		for _, entry := range recent {
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		flusher.Flush()

		for {
			select {
			case entry, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(entry)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	mux.HandleFunc("/api/pair", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req PairRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token != s.PairingToken {
			json.NewEncoder(w).Encode(PairResponse{Success: false, Error: "Invalid pairing token"})
			return
		}

		certBundle, err := s.CertManager.SignClientCSR([]byte(req.CSR))
		if err != nil {
			json.NewEncoder(w).Encode(PairResponse{Success: false, Error: err.Error()})
			return
		}
		json.NewEncoder(w).Encode(PairResponse{Success: true, Certificate: string(certBundle)})
	})

	mux.HandleFunc("/api/firewall", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(s.ConfigManager.Config.GRPCPort, ":")
		portNum := "50005"
		if len(parts) > 1 {
			portNum = parts[len(parts)-1]
		}
		httpPort := fmt.Sprintf("%d", s.HTTPListener.Addr().(*net.TCPAddr).Port)

		if runtime.GOOS == "windows" {
			cmdStr := fmt.Sprintf("netsh advfirewall firewall add rule name=\"SnapHaven Server\" dir=in action=allow protocol=TCP localport=%s,%s", portNum, httpPort)
			psCmd := fmt.Sprintf("Start-Process netsh -ArgumentList 'advfirewall firewall add rule name=\"SnapHaven Server\" dir=in action=allow protocol=TCP localport=%s,%s' -Verb RunAs", portNum, httpPort)
			err := exec.Command("powershell", "-Command", psCmd).Start()
			if err != nil {
				json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error(), "cmd": cmdStr, "os": "windows"})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Elevated firewall prompt sent to Windows!", "cmd": cmdStr, "os": "windows"})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Firewall check completed."})
	})

	server := &http.Server{Handler: mux}

	go func() {
		log.Printf("Setup Web Interface running at: %s", s.ServerURL)
		if s.ConfigManager.IsFirstRun() {
			OpenBrowser(s.ServerURL)
		}
		if err := server.Serve(s.HTTPListener); err != nil && err != http.ErrServerClosed {
			log.Printf("Setup web server error: %v", err)
		}
	}()
}

func OpenBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		log.Printf("Notice: Could not automatically open browser: %v. Please open %s manually.", err, url)
	}
}
