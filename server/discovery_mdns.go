package main

import (
	"fmt"
	"log"
	"os"

	"github.com/grandcat/zeroconf"
)

type MDNSBroadcaster struct {
	server *zeroconf.Server
}

// StartMDNSBroadcaster broadcasts the SnapHaven service over local mDNS/DNS-SD.
// It uses the service type "_photosync._tcp", matching the Android client listener.
func StartMDNSBroadcaster(serverID string, port int, version string) *MDNSBroadcaster {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "snaphaven"
	}

	instance := fmt.Sprintf("SnapHaven (%s)", hostname)
	if len(serverID) > 8 {
		instance = fmt.Sprintf("SnapHaven-%s", serverID[:8])
	}

	txtRecords := []string{
		fmt.Sprintf("server_id=%s", serverID),
		fmt.Sprintf("port=%d", port),
		fmt.Sprintf("setup_port=%d", port),
		fmt.Sprintf("version=%s", version),
		fmt.Sprintf("hostname=%s", hostname),
	}

	// Register _photosync._tcp on local. domain across all active interfaces
	srv, err := zeroconf.Register(
		instance,
		"_photosync._tcp",
		"local.",
		port,
		txtRecords,
		nil,
	)
	if err != nil {
		log.Printf("Notice: Could not start mDNS broadcaster: %v (discovery fallback will be used)", err)
		return nil
	}

	log.Printf("📡 Local network discovery broadcast active (_photosync._tcp on port %d)", port)
	return &MDNSBroadcaster{server: srv}
}

// Stop shuts down the mDNS broadcaster cleanly.
func (b *MDNSBroadcaster) Stop() {
	if b == nil || b.server == nil {
		return
	}
	b.server.Shutdown()
	log.Printf("📡 Local network discovery broadcast stopped.")
}
