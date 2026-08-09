//go:build !windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"syscall"
)

var lockFile *os.File

func EnsureSingleInstance() (uintptr, bool) {
	configPath, err := GetDefaultConfigPath()
	if err != nil {
		return 0, true
	}
	lockPath := filepath.Join(filepath.Dir(configPath), "snaphaven.lock")

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		log.Printf("Warning: Failed to open single instance lock file: %v", err)
		return 0, true
	}

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		log.Println("⚠️ Another instance of SnapHaven Server is already running on this machine.")
		file.Close()
		return 0, false
	}

	lockFile = file
	return 0, true
}
