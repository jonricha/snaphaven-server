# SnapHaven Server 🛡️📸

**SnapHaven** is a lightweight, local-first, high-performance photo and media backup solution built in Go. It allows you to securely back up photos and videos directly from your Android device to your personal computer, home server, or NAS over your local Wi-Fi network—with **zero third-party cloud dependence**.

Official Website: [https://snaphaven.app](https://snaphaven.app)

---

## Key Features

- **Privacy & Security First**: Strict Mutual TLS (mTLS) cryptographic handshakes prevent unauthorized clients or rogue servers from intercepting media.
- **QR Code Pairing**: Effortless one-time QR code scanning pairs your Android client to your local server in seconds.
- **High Performance**: Uses gRPC streaming protocol for rapid file transfers over home Wi-Fi.
- **Dynamic Port Selection**: Automatically binds to open ports with built-in setup web UI.
- **Cross-Platform**: Runs on Windows, Linux, macOS, Docker, and NAS systems (Synology, Unraid, TrueNAS).

---

## Architecture Overview

1. **Go Backend Server**: Manages local pairing tokens, issue-signed client certificates via local CA, and listens for mTLS gRPC file streams.
2. **Android Client**: Pairs via QR code scan, stores keys in Android KeyStore, and performs manual & automatic background sync.

---

## Quickstart

### Prerequisites
- [Go 1.19+](https://golang.org/doc/install)

### Running Locally

```bash
# Clone the repository
git clone https://github.com/jonricha/snaphaven-server.git
cd snaphaven-server/server

# Run the server
go run .
```

Upon launching, the server will display a local setup URL in your terminal/browser showing a QR code for initial client pairing.

---

## License

This project is open-source software licensed under the [MIT License](LICENSE).
