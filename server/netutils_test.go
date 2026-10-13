package main

import (
	"fmt"
	"net"
	"net/http"
	"testing"
)

func TestBindPortWithFallback_FreePort(t *testing.T) {
	// Let OS give an ephemeral port to find a free port
	tempLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	freePort := tempLis.Addr().(*net.TCPAddr).Port
	tempLis.Close()

	lis, boundPort, err := BindPortWithFallback(fmt.Sprintf("%d", freePort), 3, "test-server-id")
	if err != nil {
		t.Fatalf("expected successful bind, got error: %v", err)
	}
	defer lis.Close()

	if boundPort != freePort {
		t.Errorf("expected bound port %d, got %d", freePort, boundPort)
	}
}

func TestBindPortWithFallback_OccupiedPort(t *testing.T) {
	// Occupy a port
	occupiedLis, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("failed to occupy port: %v", err)
	}
	defer occupiedLis.Close()

	occupiedPort := occupiedLis.Addr().(*net.TCPAddr).Port

	// Attempt binding to the occupied port with fallback
	lis, boundPort, err := BindPortWithFallback(fmt.Sprintf("%d", occupiedPort), 5, "test-server-id")
	if err != nil {
		t.Fatalf("expected fallback bind success, got error: %v", err)
	}
	defer lis.Close()

	if boundPort == occupiedPort {
		t.Errorf("expected bound port to be different from occupied port %d", occupiedPort)
	}
	if boundPort <= occupiedPort {
		t.Errorf("expected bound port > %d, got %d", occupiedPort, boundPort)
	}
}

func TestProbeServerID_Match(t *testing.T) {
	serverID := "mock-test-id-12345"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"server_id":"%s"}`, serverID)
	})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock listener: %v", err)
	}
	defer lis.Close()

	port := lis.Addr().(*net.TCPAddr).Port
	server := &http.Server{Handler: mux}
	go server.Serve(lis)
	defer server.Close()

	foundID, err := ProbeServerID(port, 0)
	if err != nil {
		t.Fatalf("ProbeServerID failed: %v", err)
	}
	if foundID != serverID {
		t.Errorf("expected server ID %q, got %q", serverID, foundID)
	}
}
