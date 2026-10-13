package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestUnifiedMultiplexer_HTTPAndTLS(t *testing.T) {
	rootLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := rootLis.Addr().(*net.TCPAddr).Port

	mux := NewUnifiedMultiplexer(rootLis, port)
	mux.Start()
	defer mux.Close()

	// 1. Setup mock plain HTTP server
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pong-http")
	})
	httpServer := &http.Server{Handler: httpMux}
	go httpServer.Serve(mux.HTTPListener())
	defer httpServer.Close()

	// 2. Setup mock TLS server using self-signed cert from certmanager
	certDir := t.TempDir()
	cm, err := NewCertManager(certDir)
	if err != nil {
		t.Fatalf("failed to create CertManager: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cm.ServerTLSCert},
	}
	tlsLis := tls.NewListener(mux.TLSListener(), tlsConfig)

	tlsHttpMux := http.NewServeMux()
	tlsHttpMux.HandleFunc("/tls-ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pong-tls")
	})
	tlsServer := &http.Server{Handler: tlsHttpMux}
	go tlsServer.Serve(tlsLis)
	defer tlsServer.Close()

	time.Sleep(50 * time.Millisecond)

	// 3. Test plain HTTP request to /ping
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ping", port))
	if err != nil {
		t.Fatalf("plain HTTP request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	if string(body) != "pong-http" {
		t.Errorf("expected 'pong-http', got %q", string(body))
	}

	// 4. Test TLS request to /tls-ping
	tlsClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	tlsResp, err := tlsClient.Get(fmt.Sprintf("https://127.0.0.1:%d/tls-ping", port))
	if err != nil {
		t.Fatalf("TLS request failed: %v", err)
	}
	defer tlsResp.Body.Close()
	tlsBody, _ := ioutil.ReadAll(tlsResp.Body)
	if string(tlsBody) != "pong-tls" {
		t.Errorf("expected 'pong-tls', got %q", string(tlsBody))
	}
}
