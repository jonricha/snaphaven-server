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
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

type CertManager struct {
	CertDir       string
	CACert        *x509.Certificate
	CAPriv        interface{}
	ServerTLSCert tls.Certificate
	CAFingerprint string
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

	if _, err := os.Stat(serverCertPath); os.IsNotExist(err) {
		if err := cm.generateServerCert(serverCertPath, serverKeyPath); err != nil {
			return nil, fmt.Errorf("failed to generate server cert: %w", err)
		}
	}

	tlsCert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load server TLS keypair: %w", err)
	}
	cm.ServerTLSCert = tlsCert

	// Compute CA Fingerprint (SHA256)
	h := sha256.Sum256(cm.CACert.Raw)
	cm.CAFingerprint = hex.EncodeToString(h[:])

	return cm, nil
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
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:    []string{"localhost"},
	}

	// Add local IP addresses to SAN
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					template.IPAddresses = append(template.IPAddresses, ipnet.IP)
				}
			}
		}
	}

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
	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
}

// SignClientCSR signs a client CSR using the CA certificate
func (cm *CertManager) SignClientCSR(csrPEM []byte) ([]byte, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid CSR: %w", err)
	}

	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature check failed: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("failed to sign client cert: %w", err)
	}

	clientCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertBytes})
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cm.CACert.Raw})

	// Append CA cert to client cert bundle
	fullChain := append(clientCertPEM, caCertPEM...)
	return fullChain, nil
}
