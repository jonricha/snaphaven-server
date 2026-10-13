package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ProbeServerID attempts to query /api/v1/info on the given port to see if a SnapHaven instance is running.
func ProbeServerID(port int, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/info", port))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var data struct {
		ServerID string `json:"server_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.ServerID, nil
}

// BindPortWithFallback attempts to bind a TCP listener on preferredPort.
// If preferredPort is busy, it hunts sequentially up to maxFallback attempts.
// If an occupied port returns a matching serverID, it logs a zombie instance warning.
func BindPortWithFallback(preferredPort string, maxFallback int, serverID string) (net.Listener, int, error) {
	cleanPort := strings.TrimPrefix(preferredPort, ":")
	startPort, err := strconv.Atoi(cleanPort)
	if err != nil || startPort <= 0 {
		// If port is 0 or invalid, let the OS pick an ephemeral port
		lis, err := net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			return nil, 0, fmt.Errorf("failed to bind ephemeral port: %w", err)
		}
		port := lis.Addr().(*net.TCPAddr).Port
		return lis, port, nil
	}

	if maxFallback < 0 {
		maxFallback = 10
	}

	for attempt := 0; attempt <= maxFallback; attempt++ {
		targetPort := startPort + attempt
		addr := fmt.Sprintf("0.0.0.0:%d", targetPort)
		lis, err := net.Listen("tcp", addr)
		if err == nil {
			if attempt > 0 {
				log.Printf("⚠️ Preferred port :%d was busy. Successfully bound fallback port :%d", startPort, targetPort)
			}
			return lis, targetPort, nil
		}

		// Port is busy - check if it's our own zombie instance
		existingID, probeErr := ProbeServerID(targetPort, 250*time.Millisecond)
		if probeErr == nil && existingID != "" {
			if serverID != "" && existingID == serverID {
				log.Printf("⚠️ Port :%d is held by an existing SnapHaven instance with matching ServerID %s.", targetPort, existingID)
			} else {
				log.Printf("⚠️ Port :%d is occupied by another SnapHaven server instance (ServerID: %s).", targetPort, existingID)
			}
		} else {
			log.Printf("⚠️ Port :%d is occupied by another process (%v).", targetPort, err)
		}
	}

	return nil, 0, fmt.Errorf("failed to bind port %d after %d attempts", startPort, maxFallback)
}
