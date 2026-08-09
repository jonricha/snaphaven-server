//go:build !windows

package main

import (
	"fmt"
	"log"
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
	log.Println(msg)
}
