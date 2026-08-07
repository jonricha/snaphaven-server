package certs

import (
	"testing"
)

func TestCertSetup(t *testing.T) {
	err := writecerts("My org", "CA", "BC", "Surrey", "6198 Killarney Dr.", "V3S 5W9", []string{"*.test.example.com"})
	if err != nil {
		t.Fatalf("Failed to write certs with: %v", err)
	}
}
