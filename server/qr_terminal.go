package main

import (
	"fmt"
	"log"

	"github.com/skip2/go-qrcode"
)

// GenerateTerminalQRCode returns an ANSI half-block string of the given JSON payload.
func GenerateTerminalQRCode(payload string) (string, error) {
	qr, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		return "", err
	}
	return qr.ToSmallString(false), nil
}

// PrintTerminalQRCode displays a banner and interactive QR code in the terminal.
func PrintTerminalQRCode(payload string, dashboardURL string, caFingerprint string) {
	qrArt, err := GenerateTerminalQRCode(payload)
	if err != nil {
		log.Printf("⚠️ Failed to generate terminal QR code: %v", err)
		return
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println("🛡️ SnapHaven Server Ready for Pairing (Headless / Terminal Mode)")
	fmt.Printf("🌐 Active Web Dashboard: %s\n", dashboardURL)
	if caFingerprint != "" {
		fmt.Printf("🔒 CA Fingerprint:       %s\n", caFingerprint)
	}
	fmt.Println("📱 Scan the QR code below using the SnapHaven Android App to pair your device:")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Print(qrArt)
	fmt.Println("================================================================================")
	fmt.Println()
}
