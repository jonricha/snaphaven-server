//go:build windows

package main

import (
	"log"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex  = kernel32.NewProc("CreateMutexW")
	procGetLastError = kernel32.NewProc("GetLastError")
)

const ERROR_ALREADY_EXISTS = 183

func EnsureSingleInstance() (uintptr, bool) {
	mutexName, err := syscall.UTF16PtrFromString("Global\\SnapHavenServerSingleInstanceMutex")
	if err != nil {
		return 0, true
	}

	ret, _, _ := procCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(mutexName)))
	lastErr, _, _ := procGetLastError.Call()

	if lastErr == ERROR_ALREADY_EXISTS {
		log.Println("⚠️ Another instance of SnapHaven Server is already running.")
		return ret, false
	}
	return ret, true
}
