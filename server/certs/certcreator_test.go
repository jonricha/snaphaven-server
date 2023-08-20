package certs

import "testing"

func TestCertSetup(t *testing.T) {
	serverTLSConf, clientTLSConf, err := certsetup("My org", "CA", "BC", "Surrey", "6198 Killarney Dr.", "V3S 5W9")
	if err != nil {
		t.Fatalf("certsetup failed: %v", err)
	}
	if serverTLSConf == nil {
		t.Fatal("serverTLSConf == nil!")
	}
	if clientTLSConf == nil {
		t.Fatal("clientTLSConf == nil!")
	}
}
