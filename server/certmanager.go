package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CertManager struct {
	mu            sync.RWMutex
	CertDir       string
	CACert        *x509.Certificate
	CAPriv        interface{}
	ServerTLSCert tls.Certificate
	ServerCert    *x509.Certificate
	CAFingerprint string
	lastIPCheck   time.Time
}

func getActiveIPs() []net.IP {
	var ips []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				ips = append(ips, ip4)
			}
		}
	}
	return ips
}

func NewCertManager(certDir string) (*CertManager, error) {
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return nil, err
	}

	cm := &CertManager{CertDir: certDir}

	caCertPath := filepath.Join(certDir, "ca_cert.pem")
	caKeyPath := filepath.Join(certDir, "ca_key.pem")

	if _, err := os.Stat(caCertPath); os.IsNotExist(err) {
		if err := cm.generateCA(caCertPath, caKeyPath); err != nil {
			return nil, fmt.Errorf("failed to generate CA: %w", err)
		}
	}

	if err := cm.loadCA(caCertPath, caKeyPath); err != nil {
		return nil, fmt.Errorf("failed to load CA: %w", err)
	}

	serverCertPath := filepath.Join(certDir, "server_cert.pem")
	serverKeyPath := filepath.Join(certDir, "server_key.pem")

	if _, err := os.Stat(serverCertPath); err == nil {
		_ = cm.loadServerCert(serverCertPath, serverKeyPath)
	}

	if cm.needsRegenerationLocked() {
		if err := cm.generateServerCert(serverCertPath, serverKeyPath); err != nil {
			return nil, fmt.Errorf("failed to generate server cert: %w", err)
		}
	}

	// Compute CA Fingerprint (SHA256)
	h := sha256.Sum256(cm.CACert.Raw)
	cm.CAFingerprint = hex.EncodeToString(h[:])
	cm.lastIPCheck = time.Now()

	return cm, nil
}

func (cm *CertManager) loadServerCert(certPath, keyPath string) error {
	tlsCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return err
	}
	cm.ServerTLSCert = tlsCert

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("failed to decode server cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	cm.ServerCert = cert
	return nil
}

func (cm *CertManager) needsRegenerationLocked() bool {
	if cm.ServerCert == nil || len(cm.ServerTLSCert.Certificate) == 0 {
		return true
	}
	// Expired or expiring within 30 days
	if time.Now().After(cm.ServerCert.NotAfter.AddDate(0, 0, -30)) {
		return true
	}

	// Check if all active IPv4 addresses are present in the certificate's SAN IP list
	activeIPs := getActiveIPs()
	for _, aip := range activeIPs {
		found := false
		for _, cip := range cm.ServerCert.IPAddresses {
			if cip.Equal(aip) {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// EnsureServerCertValid verifies that the server certificate covers all current network interfaces.
// If not, it regenerates the certificate using the existing Root CA.
func (cm *CertManager) EnsureServerCertValid() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.lastIPCheck = time.Now()
	if cm.needsRegenerationLocked() {
		serverCertPath := filepath.Join(cm.CertDir, "server_cert.pem")
		serverKeyPath := filepath.Join(cm.CertDir, "server_key.pem")
		return cm.generateServerCert(serverCertPath, serverKeyPath)
	}
	return nil
}

// GetCertificate dynamically provides the server TLS certificate during TLS handshakes,
// automatically refreshing it if the local network IP has changed (e.g. DHCP lease renewal).
func (cm *CertManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cm.mu.RLock()
	needsCheck := time.Since(cm.lastIPCheck) > 10*time.Second
	cm.mu.RUnlock()

	if needsCheck {
		cm.mu.Lock()
		if time.Since(cm.lastIPCheck) > 10*time.Second {
			cm.lastIPCheck = time.Now()
			if cm.needsRegenerationLocked() {
				serverCertPath := filepath.Join(cm.CertDir, "server_cert.pem")
				serverKeyPath := filepath.Join(cm.CertDir, "server_key.pem")
				if err := cm.generateServerCert(serverCertPath, serverKeyPath); err != nil {
					log.Printf("⚠️ Failed to auto-refresh server TLS certificate: %v", err)
				}
			}
		}
		cm.mu.Unlock()
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return &cm.ServerTLSCert, nil
}

func (cm *CertManager) generateCA(certPath, keyPath string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serialNumberNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumberNumber,
		Subject: pkix.Name{
			Organization: []string{"SnapHaven Local CA"},
			CommonName:   "SnapHaven Root CA",
		},
		NotBefore:             time.Now().Add(-10 * time.Minute),
		NotAfter:              time.Now().AddDate(10, 0, 0), // 10 years
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return err
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}

	return nil
}

func (cm *CertManager) loadCA(certPath, keyPath string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("failed to decode CA cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	cm.CACert = cert

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode CA key PEM")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return err
	}
	cm.CAPriv = key
	return nil
}

func (cm *CertManager) generateServerCert(certPath, keyPath string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"SnapHaven Server"},
			CommonName:   "SnapHaven Server",
		},
		NotBefore:   time.Now().Add(-10 * time.Minute),
		NotAfter:    time.Now().AddDate(5, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:    []string{"localhost"},
	}

	// Add local hostname and mDNS domains to SAN DNSNames
	if h, err := os.Hostname(); err == nil && h != "" {
		template.DNSNames = append(template.DNSNames, h)
		template.DNSNames = append(template.DNSNames, h+".local")
	}
	template.DNSNames = append(template.DNSNames, "snaphaven.local")

	// Collect unique IPs: preserve any previously known SAN IPs + current active IPs
	ipMap := make(map[string]net.IP)
	for _, ip := range template.IPAddresses {
		ipMap[ip.String()] = ip
	}

	// Preserve prior SAN IPs if available (maintains backward compatibility across interface toggles)
	if cm.ServerCert != nil {
		for _, ip := range cm.ServerCert.IPAddresses {
			ipMap[ip.String()] = ip
		}
	}

	// Add all current active interface IPs
	for _, ip := range getActiveIPs() {
		ipMap[ip.String()] = ip
	}

	// Also add all interface addresses from net.InterfaceAddrs()
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				ipMap[ipnet.IP.String()] = ipnet.IP
			}
		}
	}

	var allIPs []net.IP
	for _, ip := range ipMap {
		allIPs = append(allIPs, ip)
	}
	template.IPAddresses = allIPs

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, cm.CACert, &priv.PublicKey, cm.CAPriv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return err
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}

	if err := cm.loadServerCert(certPath, keyPath); err != nil {
		return err
	}

	log.Printf("🔄 Server TLS certificate generated/updated with IP SANs: %v", template.IPAddresses)
	return nil
}

// SignClientCSR signs a client CSR using the CA certificate and returns the cert chain PEM and the issued certificate
func (cm *CertManager) SignClientCSR(csrPEM []byte) ([]byte, *x509.Certificate, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("failed to decode CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid CSR: %w", err)
	}

	if err := csr.CheckSignature(); err != nil {
		return nil, nil, fmt.Errorf("CSR signature check failed: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	clientTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-10 * time.Minute),
		NotAfter:     time.Now().AddDate(2, 0, 0), // Valid for 2 years
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	clientCertBytes, err := x509.CreateCertificate(rand.Reader, &clientTemplate, cm.CACert, csr.PublicKey, cm.CAPriv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign client cert: %w", err)
	}

	issuedCert, err := x509.ParseCertificate(clientCertBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse issued client cert: %w", err)
	}

	clientCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertBytes})
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cm.CACert.Raw})

	// Append CA cert to client cert bundle
	fullChain := append(clientCertPEM, caCertPEM...)
	return fullChain, issuedCert, nil
}
