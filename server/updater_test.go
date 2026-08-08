package main

import (
	"testing"
)

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input  string
		major  int
		minor  int
		patch  int
	}{
		{"v1.2.3", 1, 2, 3},
		{"1.0.0", 1, 0, 0},
		{"v2.15.4-beta1", 2, 15, 4},
		{"v0.0.1+build123", 0, 0, 1},
		{"invalid", 0, 0, 0},
	}

	for _, tt := range tests {
		maj, min, pat := parseSemver(tt.input)
		if maj != tt.major || min != tt.minor || pat != tt.patch {
			t.Errorf("parseSemver(%q) = (%d, %d, %d), expected (%d, %d, %d)",
				tt.input, maj, min, pat, tt.major, tt.minor, tt.patch)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		remote  string
		current string
		expected bool
	}{
		{"v1.0.1", "v1.0.0", true},
		{"v1.1.0", "v1.0.9", true},
		{"v2.0.0", "v1.99.99", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v1.0.0-dev", "v1.0.0", false},
		{"v2.0.0", "v0.0.0-dev", true},
	}

	for _, tt := range tests {
		result := isNewerVersion(tt.remote, tt.current)
		if result != tt.expected {
			t.Errorf("isNewerVersion(%q, %q) = %v, expected %v",
				tt.remote, tt.current, result, tt.expected)
		}
	}
}

func TestUpdateManagerStatus(t *testing.T) {
	um := NewUpdateManager("jonricha", "snaphaven-server")
	status := um.GetStatus()
	if status.State != StateIdle {
		t.Errorf("Expected initial state to be StateIdle, got %v", status.State)
	}
	if status.CurrentVer != GetVersion() {
		t.Errorf("Expected CurrentVer to match GetVersion() %q, got %q", GetVersion(), status.CurrentVer)
	}
}
