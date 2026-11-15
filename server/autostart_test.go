package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigPathResolution(t *testing.T) {
	path, err := GetDefaultConfigPath()
	if err != nil {
		t.Fatalf("Failed to get default config path: %v", err)
	}

	if path == "" {
		t.Errorf("Config path is empty")
	}

	base := filepath.Base(path)
	if base != "config.json" {
		t.Errorf("Expected config filename config.json, got: %s", base)
	}
}

func TestEnsureSingleInstance(t *testing.T) {
	_, isSingle := EnsureSingleInstance()
	if !isSingle {
		t.Errorf("Expected first instance call to return true")
	}
}

func TestAutoStartConfig(t *testing.T) {
	if IsMacAppStore() {
		if exe, err := os.Executable(); err == nil && !strings.Contains(exe, ".app/Contents/MacOS/") {
			t.Skip("SMAppService.mainAppService requires a macOS .app bundle")
		}
	}
	err := SetAutoStartConfig(false)
	if err != nil {
		t.Fatalf("SetAutoStartConfig(false) failed: %v", err)
	}

	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		err = SetAutoStartConfig(true)
		if err != nil {
			t.Fatalf("SetAutoStartConfig(true) failed: %v", err)
		}
		// Clean up after test
		SetAutoStartConfig(false)
	}
}
