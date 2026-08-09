//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

func ShowAboutDialog(version, commit, buildTime string) {
	msg := fmt.Sprintf(
		"SnapHaven Server\n"+
			"--------------------------------------------------\n"+
			"Version: %s\n"+
			"Git Commit: %s\n"+
			"Build Time: %s\n"+
			"Author: Jonathan Richardson\n"+
			"License: MIT\n"+
			"Repository: https://github.com/jonricha/snaphaven-server\n"+
			"--------------------------------------------------\n"+
			"Secure mTLS Photo & File Synchronization Server",
		version, commit, buildTime,
	)

	user32 := syscall.NewLazyDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	captionPtr, _ := syscall.UTF16PtrFromString("About SnapHaven Server")
	textPtr, _ := syscall.UTF16PtrFromString(msg)
	const MB_OK = 0x00000000
	const MB_ICONINFORMATION = 0x00000040
	messageBox.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(captionPtr)), uintptr(MB_OK|MB_ICONINFORMATION))
}
