package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/jonricha/snaphaven-server/snaphaven"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func TestUnifiedServer_SinglePortEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	syncDir := filepath.Join(tempDir, "photos")
	_ = os.MkdirAll(syncDir, 0755)

	// 1. Initialize Configuration with ephemeral preferred port to guarantee clean bind
	configMgr, err := NewConfigManager(configPath)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}
	configMgr.Config.SyncDirectory = syncDir
	configMgr.Config.GRPCPort = "0" // Pick free port dynamically for test
	configMgr.Config.CertDirectory = filepath.Join(tempDir, "certs")
	_ = configMgr.Save()

	// 2. Initialize CertManager and DeviceManager
	certMgr, err := NewCertManager(configMgr.Config.CertDirectory)
	if err != nil {
		t.Fatalf("failed to create CertManager: %v", err)
	}
	deviceMgr, err := NewDeviceManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create DeviceManager: %v", err)
	}

	// 3. Start ServerManager (starts unified port, cmux, mDNS, and gRPC)
	srvMgr := NewServerManager(configMgr, certMgr, deviceMgr)
	if err := srvMgr.Start(); err != nil {
		t.Fatalf("failed to start ServerManager: %v", err)
	}
	defer srvMgr.Shutdown()

	boundPort := srvMgr.BoundPort()
	if boundPort <= 0 {
		t.Fatalf("expected positive bound port, got %d", boundPort)
	}

	// 4. Start SetupServer on the multiplexed HTTP listener
	updaterMgr := NewUpdateManager("", "")
	setupServer, err := NewSetupServer(srvMgr.HTTPListener(), boundPort, certMgr, deviceMgr, configMgr, srvMgr, updaterMgr)
	if err != nil {
		t.Fatalf("failed to create SetupServer: %v", err)
	}
	srvMgr.AttachSetupServer(setupServer)
	setupServer.Start()

	time.Sleep(100 * time.Millisecond)

	// 5. Verify plain HTTP endpoints over the single unified port
	// Check /api/v1/info
	infoURL := fmt.Sprintf("http://127.0.0.1:%d/api/v1/info", boundPort)
	resp, err := http.Get(infoURL)
	if err != nil {
		t.Fatalf("failed to query /api/v1/info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 from /api/v1/info, got %d", resp.StatusCode)
	}

	var infoData struct {
		ServerID  string `json:"server_id"`
		Port      int    `json:"port"`
		SetupPort int    `json:"setup_port"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&infoData); err != nil {
		t.Fatalf("failed to decode info json: %v", err)
	}

	if infoData.Port != boundPort || infoData.SetupPort != boundPort {
		t.Errorf("expected both port and setup_port to equal boundPort %d, got port=%d setup_port=%d",
			boundPort, infoData.Port, infoData.SetupPort)
	}

	// 6. Verify Pairing Payload format & backward compatibility keys ("port" AND "sport")
	payloadJSON := setupServer.GetPairingJSON()
	var payloadMap map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &payloadMap); err != nil {
		t.Fatalf("failed to parse pairing payload: %v", err)
	}

	if int(payloadMap["port"].(float64)) != boundPort {
		t.Errorf("expected payload 'port' to be %d, got %v", boundPort, payloadMap["port"])
	}
	if int(payloadMap["sport"].(float64)) != boundPort {
		t.Errorf("expected backward-compatible payload 'sport' to be %d, got %v", boundPort, payloadMap["sport"])
	}

	// Verify terminal QR generation from payload
	ansiQR, err := GenerateTerminalQRCode(payloadJSON)
	if err != nil || len(ansiQR) == 0 {
		t.Fatalf("failed to generate terminal ANSI QR code: %v", err)
	}

	// 7. Verify mTLS gRPC Ping over the exact same unified port
	// Generate client key and CSR
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}

	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "IntegrationTestClient"},
	}
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, clientKey)
	if err != nil {
		t.Fatalf("failed to create client CSR: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})

	clientCertBundlePEM, _, err := certMgr.SignClientCSR(csrPEM)
	if err != nil {
		t.Fatalf("failed to sign client CSR: %v", err)
	}

	clientKeyBytes, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatalf("failed to marshal client key: %v", err)
	}
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyBytes})

	clientTLSCert, err := tls.X509KeyPair(clientCertBundlePEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("failed to load client TLS keypair: %v", err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AddCert(certMgr.CACert)

	clientCreds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientTLSCert},
		RootCAs:      caCertPool,
		ServerName:   "127.0.0.1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, fmt.Sprintf("127.0.0.1:%d", boundPort), grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("failed to dial gRPC server: %v", err)
	}
	defer conn.Close()

	grpcClient := pb.NewSnapHavenClient(conn)
	pingReply, err := grpcClient.Ping(ctx, &pb.PingRequest{ClientVersion: "2.0.0-test"})
	if err != nil {
		t.Fatalf("mTLS gRPC Ping failed over unified port %d: %v", boundPort, err)
	}

	if pingReply.GetServerVersion() == "" {
		t.Errorf("expected non-empty server version from PingReply")
	}

	// 8. Verify Pausing
	srvMgr.Stop()
	if srvMgr.IsRunning() {
		t.Errorf("expected server to report not running after Stop()")
	}

	// While paused, plain HTTP dashboard should still work!
	httpResp, err := http.Get(infoURL)
	if err != nil || httpResp.StatusCode != http.StatusOK {
		t.Errorf("HTTP dashboard should remain accessible when gRPC is paused, got err=%v code=%v",
			err, httpResp.StatusCode)
	}
	if httpResp != nil {
		httpResp.Body.Close()
	}

	// 9. Verify Resuming
	if err := srvMgr.Start(); err != nil {
		t.Fatalf("failed to resume server: %v", err)
	}
	if !srvMgr.IsRunning() {
		t.Errorf("expected server to report running after Start()")
	}
}

func TestIsLocalMachineRequest(t *testing.T) {
	loopbackReq := &http.Request{RemoteAddr: "127.0.0.1:54321"}
	if !IsLocalMachineRequest(loopbackReq) {
		t.Errorf("expected loopback IPv4 request to be recognized as local")
	}

	ipv6LoopbackReq := &http.Request{RemoteAddr: "[::1]:54321"}
	if !IsLocalMachineRequest(ipv6LoopbackReq) {
		t.Errorf("expected loopback IPv6 request to be recognized as local")
	}

	externalReq := &http.Request{RemoteAddr: "203.0.113.42:54321"}
	if IsLocalMachineRequest(externalReq) {
		t.Errorf("expected external IP 203.0.113.42 to be rejected")
	}

	localIPs := GetLocalIPs()
	if len(localIPs) > 0 {
		lanSelfReq := &http.Request{RemoteAddr: fmt.Sprintf("%s:54321", localIPs[0])}
		if !IsLocalMachineRequest(lanSelfReq) {
			t.Errorf("expected host's own interface IP %s to be recognized as local", localIPs[0])
		}
	}

	ts := GetTrayStrings()
	if ts.OpenPhotosFolder == "" {
		t.Errorf("expected TrayStrings.OpenPhotosFolder to be non-empty")
	}
}

