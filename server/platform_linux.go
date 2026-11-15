//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func PickFolderPlatform(initialDir string) (string, string, bool, error) {
	if zenityPath, err := exec.LookPath("zenity"); err == nil {
		args := []string{"--file-selection", "--directory", "--title=Select Sync Folder"}
		if initialDir != "" {
			args = append(args, "--filename="+strings.TrimRight(initialDir, "/")+"/")
		}
		cmd := exec.Command(zenityPath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				return "", "", true, nil
			}
			return "", "", false, fmt.Errorf("zenity failed: %v (%s)", err, strings.TrimSpace(stderr.String()))
		}
		selected := strings.TrimSpace(stdout.String())
		if selected == "" {
			return "", "", true, nil
		}
		return selected, "", false, nil
	}

	if kdialogPath, err := exec.LookPath("kdialog"); err == nil {
		args := []string{"--getexistingdirectory"}
		if initialDir != "" {
			args = append(args, initialDir)
		}
		args = append(args, "--title", "Select Sync Folder")
		cmd := exec.Command(kdialogPath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				return "", "", true, nil
			}
			return "", "", false, fmt.Errorf("kdialog failed: %v (%s)", err, strings.TrimSpace(stderr.String()))
		}
		selected := strings.TrimSpace(stdout.String())
		if selected == "" {
			return "", "", true, nil
		}
		return selected, "", false, nil
	}

	return "", "", false, errors.New("no native folder picker found (install zenity or kdialog), please enter the path manually")
}

func RestoreSecurityScopedBookmarkPlatform(bookmarkBase64 string) (string, error) {
	return "", nil
}

func OpenURLPlatform(rawURL string) error {
	return fmt.Errorf("not implemented on linux")
}

func OpenFolderPlatform(absPath string) error {
	return fmt.Errorf("not implemented on linux")
}

func SetAutoStartDarwinMAS(enabled bool) error {
	return nil
}

func ShowAboutDialogPlatform(title, message string) {}
