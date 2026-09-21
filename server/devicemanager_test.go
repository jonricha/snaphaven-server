package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net"
	"testing"

	pb "github.com/jonricha/snaphaven-server/snaphaven"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func TestDeviceManager_CRUD(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "devicemanager_test")
	if err != nil {
		t.Fatal(err)
	}

	dm, err := NewDeviceManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create DeviceManager: %v", err)
	}

	serial1 := "0102030405060708"
	fp1 := "abcdef123456"

	// 1. Register device
	dev := dm.RegisterDevice(serial1, fp1, "My Pixel", "Google Pixel 8")
	if dev == nil || dev.Name != "My Pixel" {
		t.Fatalf("expected registered device name 'My Pixel', got %+v", dev)
	}

	if dm.IsRevoked(serial1) {
		t.Fatalf("expected device not revoked initially")
	}

	// 2. Revoke device
	if !dm.RevokeDevice(serial1) {
		t.Fatalf("failed to revoke device")
	}
	if !dm.IsRevoked(serial1) {
		t.Fatalf("expected device to be revoked")
	}

	// 3. Unrevoke device
	if !dm.UnrevokeDevice(serial1) {
		t.Fatalf("failed to unrevoke device")
	}
	if dm.IsRevoked(serial1) {
		t.Fatalf("expected device to be active again")
	}

	// 4. Rename device
	if !dm.RenameDevice(serial1, "Pixel Renamed") {
		t.Fatalf("failed to rename device")
	}

	// Reload from disk to verify persistence
	dm2, err := NewDeviceManager(tempDir)
	if err != nil {
		t.Fatalf("failed to reload DeviceManager: %v", err)
	}
	devs := dm2.GetAllDevices()
	if len(devs) != 1 || devs[0].Name != "Pixel Renamed" {
		t.Fatalf("expected 1 persisted device named 'Pixel Renamed', got %+v", devs)
	}

	// 5. Delete device
	if !dm2.DeleteDevice(serial1) {
		t.Fatalf("failed to delete device")
	}
	if len(dm2.GetAllDevices()) != 0 {
		t.Fatalf("expected 0 devices after deletion")
	}
}

func TestDeviceRevocation_mTLSHandshake(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "revocation_server_test")
	if err != nil {
		t.Fatal(err)
	}
	certdir, err := ioutil.TempDir("", "revocation_cert_test")
	if err != nil {
		t.Fatal(err)
	}

	cm, err := NewCertManager(certdir)
	if err != nil {
		t.Fatal(err)
	}

	dm, err := NewDeviceManager(tempdir)
	if err != nil {
		t.Fatal(err)
	}

	port := ":0"
	s, lis := RegisterServer(tempdir, port, cm, dm)
	defer s.GracefulStop()

	go func() {
		_ = s.Serve(lis)
	}()

	actualPort := fmt.Sprintf(":%d", lis.Addr().(*net.TCPAddr).Port)

	// Create client CSR and sign with CA
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "RevocableClient"},
	}
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})

	clientCertBundlePEM, issuedCert, err := cm.SignClientCSR(csrPEM)
	if err != nil {
		t.Fatal(err)
	}

	clientKeyBytes, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyBytes})

	tlsCert, err := tls.X509KeyPair(clientCertBundlePEM, clientKeyPEM)
	if err != nil {
		t.Fatal(err)
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(cm.CACert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		RootCAs:      caPool,
		ServerName:   "localhost",
	}

	creds := credentials.NewTLS(tlsConfig)
	conn, err := grpc.Dial("localhost"+actualPort, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewSnapHavenClient(conn)

	// 1. Initial Ping should succeed (active client)
	_, err = client.Ping(context.Background(), &pb.PingRequest{ClientVersion: "1.0.0"})
	if err != nil {
		t.Fatalf("expected ping to succeed for active client: %v", err)
	}

	// Verify auto-registered
	serialHex := NormalizeSerial(issuedCert.SerialNumber)
	if dm.IsRevoked(serialHex) {
		t.Fatalf("device should not be revoked")
	}

	// 2. Revoke the client
	dm.RevokeDevice(serialHex)

	// New connection from the revoked client should fail TLS handshake / peer verification
	connRevoked, err := grpc.Dial("localhost"+actualPort, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	defer connRevoked.Close()

	clientRevoked := pb.NewSnapHavenClient(connRevoked)
	_, err = clientRevoked.Ping(context.Background(), &pb.PingRequest{ClientVersion: "1.0.0"})
	if err == nil {
		t.Fatalf("expected ping to FAIL for revoked client, but it succeeded!")
	}
	t.Logf("Revocation successfully blocked connection: %v", err)
}
