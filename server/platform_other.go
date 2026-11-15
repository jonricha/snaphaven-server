//go:build !darwin && !windows && !linux

package main

import "errors"

func PickFolderPlatform(initialDir string) (string, string, bool, error) {
	return "", "", false, errors.New("native folder picker is not available on this platform")
}

func RestoreSecurityScopedBookmarkPlatform(bookmarkBase64 string) (string, error) {
	return "", nil
}

func OpenURLPlatform(rawURL string) error {
	return errors.New("not implemented on this platform")
}

func OpenFolderPlatform(absPath string) error {
	return errors.New("not implemented on this platform")
}

func SetAutoStartDarwinMAS(enabled bool) error {
	return nil
}

func ShowAboutDialogPlatform(title, message string) {}
