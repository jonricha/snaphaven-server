package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCertManager_DHCPChange_AutoRegeneration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "certmanager_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. Initialize CertManager
	cm, err := NewCertManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create CertManager: %v", err)
	}

	initialCAFingerprint := cm.CAFingerprint
	initialCASerial := cm.CACert.SerialNumber.String()
	initialServerCert := cm.ServerCert

	if initialServerCert == nil {
		t.Fatalf("expected cm.ServerCert to be populated")
	}

	// 2. Issue a client certificate signed by this CA (simulating an already-paired mobile device)
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}
	clientCSRTemplate := x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "TestClient"},
	}
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &clientCSRTemplate, clientKey)
	if err != nil {
		t.Fatalf("failed to create client CSR: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})
	clientBundlePEM, clientCert, err := cm.SignClientCSR(csrPEM)
	if err != nil {
		t.Fatalf("SignClientCSR failed: %v", err)
	}
	if len(clientBundlePEM) == 0 || clientCert == nil {
		t.Fatalf("expected valid client cert bundle")
	}

	// Verify client cert is accepted by CA pool
	caPool := x509.NewCertPool()
	caPool.AddCert(cm.CACert)
	_, err = clientCert.Verify(x509.VerifyOptions{Roots: caPool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	if err != nil {
		t.Fatalf("client cert verification failed against CA: %v", err)
	}

	// 3. Simulate a DHCP IP change:
	// Suppose the server was originally on 192.168.1.66 and the new DHCP IP is 192.168.1.75
	oldIP := net.ParseIP("192.168.1.66")
	newDHCP_IP := net.ParseIP("192.168.1.75")

	// Artificially modify cm.ServerCert to only contain oldIP and localhost
	cm.ServerCert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), oldIP}

	// Check if CertManager recognizes that newDHCP_IP is missing
	foundNew := false
	for _, ip := range cm.ServerCert.IPAddresses {
		if ip.Equal(newDHCP_IP) {
			foundNew = true
			break
		}
	}
	if foundNew {
		t.Fatalf("did not expect new DHCP IP in test setup yet")
	}

	// Force generation of certificate including new IP while preserving old IP
	serverCertPath := filepath.Join(tempDir, "server_cert.pem")
	serverKeyPath := filepath.Join(tempDir, "server_key.pem")

	// Temporarily inject the new IP into generation
	err = cm.generateServerCert(serverCertPath, serverKeyPath)
	if err != nil {
		t.Fatalf("failed to generate updated server cert: %v", err)
	}

	// 4. Verify Root CA was NOT touched
	if cm.CAFingerprint != initialCAFingerprint {
		t.Fatalf("CA fingerprint changed! Old: %s, New: %s", initialCAFingerprint, cm.CAFingerprint)
	}
	if cm.CACert.SerialNumber.String() != initialCASerial {
		t.Fatalf("CA Serial changed! Old: %s, New: %s", initialCASerial, cm.CACert.SerialNumber.String())
	}

	// 5. Verify the client cert still verifies cleanly with the CA
	_, err = clientCert.Verify(x509.VerifyOptions{Roots: caPool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	if err != nil {
		t.Fatalf("previously paired client cert failed verification after server cert renewal: %v", err)
	}

	// 6. Verify that GetCertificate serves the updated certificate
	tlsCert, err := cm.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate returned error: %v", err)
	}
	if len(tlsCert.Certificate) == 0 {
		t.Fatalf("GetCertificate returned empty certificate chain")
	}

	parsedLeaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse leaf certificate from GetCertificate: %v", err)
	}

	// Check that the old IP is preserved
	hasOldIP := false
	for _, ip := range parsedLeaf.IPAddresses {
		if ip.Equal(oldIP) {
			hasOldIP = true
			break
		}
	}
	if !hasOldIP {
		t.Errorf("expected preserved old IP %v in updated server cert SANs: %v", oldIP, parsedLeaf.IPAddresses)
	}
}

func TestCertManager_NeedsRegeneration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "certmanager_test_regen_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cm, err := NewCertManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create CertManager: %v", err)
	}

	// Active IPs should be covered
	cm.mu.Lock()
	if cm.needsRegenerationLocked() {
		t.Errorf("expected newly created cert to not need regeneration immediately")
	}

	// Simulate expired certificate
	cm.ServerCert.NotAfter = time.Now().Add(-1 * time.Hour)
	if !cm.needsRegenerationLocked() {
		t.Errorf("expected expired cert to require regeneration")
	}
	cm.mu.Unlock()

	// EnsureServerCertValid should fix it
	if err := cm.EnsureServerCertValid(); err != nil {
		t.Fatalf("EnsureServerCertValid failed: %v", err)
	}

	cm.mu.Lock()
	if cm.needsRegenerationLocked() {
		t.Errorf("expected renewed cert to be valid")
	}
	cm.mu.Unlock()
}
