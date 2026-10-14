# SnapHaven NAS & Headless Deployment Guide

This guide explains how to run **SnapHaven Server** on a Network Attached Storage (NAS), Raspberry Pi, or home server.

---

## 1. Architecture Highlights

* **Single Unified Port (`:50005`):** Both Mutual TLS (mTLS) photo sync and the Web Dashboard / Pairing API operate simultaneously over port `50005`.
* **Automatic Port Hunting:** If port `50005` is busy, the server automatically hunts the next free port (`50006`, `50007`, etc.) as a single unit without crashing.
* **Local Network Discovery (mDNS):** Broadcasts `_photosync._tcp` on your local network so the Android client automatically finds the server even if DHCP changes the IP address or the port shifts.
* **Headless Terminal QR Code:** When running in Docker or SSH, an ANSI QR code is automatically printed directly to the terminal stdout and saved to disk.
* **100% Backward Compatible:** The pairing payload populates both `"port"` and `"sport"`, ensuring existing beta clients pair and sync seamlessly.

---

## 2. Deployment Scenarios

### Option A: Using Your Existing Legacy NAS (e.g. D-Link DNS-323)

Because legacy devices like the DNS-323 lack the CPU power and RAM (64 MB) to run modern Go runtimes, you can use one of these two proven methods:

#### Method 1: Run on PC with a Mapped Drive
1. Run SnapHaven Server on your Windows or Mac PC.
2. In the dashboard settings or launch command, set your sync directory to the mapped NAS drive:
   ```cmd
   snaphaven-server.exe --dir "Z:\Photos"
   ```
   or UNC path:
   ```cmd
   snaphaven-server.exe --dir "\\DNS-323\Volume_1\Photos"
   ```

#### Method 2: Cheap 24/7 Companion (Raspberry Pi or Mini-PC)
1. Use an inexpensive Raspberry Pi (or refurbished Intel N100 mini-PC) connected to your router.
2. Mount your DNS-323 share on boot via `/etc/fstab`:
   ```bash
   //192.168.1.xxx/Volume_1 /mnt/nas/photos cifs username=admin,password=secret,uid=1000,gid=1000 0 0
   ```
3. Run SnapHaven Server pointing to the mount:
   ```bash
   ./snaphaven-server --headless --dir /mnt/nas/photos
   ```

---

### Option B: Modern NAS Deployment via Docker (Synology, QNAP, TrueNAS, unRAID)

Modern NAS devices have built-in Docker support.

#### Using Docker Compose
1. Place `docker-compose.yml` on your NAS (or use the one in this repository):
   ```yaml
   version: "3.8"
   services:
     snaphaven:
       image: ghcr.io/jonricha/snaphaven-server:latest
       build:
         context: .
         dockerfile: Dockerfile
       container_name: snaphaven-server
       restart: unless-stopped
       ports:
         - "50005:50005"
       environment:
         - HEADLESS=true
         - SNAP_DIR=/data/photos
         - SNAP_PORT=:50005
       volumes:
         - /volume1/photos:/data/photos
         - /volume1/docker/snaphaven/config:/config
   ```
2. Start the container:
   - To use the pre-built image from GitHub Container Registry:
     ```bash
     docker compose up -d
     ```
   - To build and run locally from source:
     ```bash
     docker compose up --build -d
     ```

---

## 3. How to Pair in Headless / NAS Environments

You have two simple ways to scan the pairing QR code when there is no physical monitor:

### Method 1: Web Browser (From any device on your Wi-Fi)
1. Open your browser on any laptop, tablet, or desktop connected to your home network:
   ```
   http://<NAS_IP>:50005
   ```
2. The SnapHaven dashboard appears showing the live pairing QR code.
3. Open the SnapHaven Android app, tap **Scan QR Code**, and scan your computer's monitor.

### Method 2: Terminal / Docker Logs
1. View container logs:
   ```bash
   docker logs snaphaven-server
   ```
   or in SSH:
   ```bash
   ./snaphaven-server qr
   ```
2. The terminal renders high-contrast ANSI block characters forming the QR code directly in your console.
3. Point your Android phone camera at your terminal screen to complete pairing.

---

## 4. CLI Commands Quick Reference

| Command | Action |
| :--- | :--- |
| `snaphaven-server qr` | Renders the mobile pairing QR code directly in the terminal |
| `snaphaven-server status` | Displays active server status, bound port, and web dashboard URL |
| `snaphaven-server open` | Opens the web dashboard in your default desktop browser |
| `snaphaven-server --headless` | Starts server in headless background mode (prints QR code on boot) |
| `snaphaven-server --dir <path>` | Specifies custom directory for synced photos |
| `snaphaven-server --port <port>` | Specifies custom starting port (default `:50005`) |
