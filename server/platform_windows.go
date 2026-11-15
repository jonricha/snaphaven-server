//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func PickFolderPlatform(initialDir string) (string, string, bool, error) {
	escapedInit := strings.ReplaceAll(initialDir, "'", "''")
	psScript := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$owner = New-Object System.Windows.Forms.Form -Property @{ TopMost = $true }
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = 'Choose the folder where SnapHaven Server will store backed-up photos and videos.'
$dialog.ShowNewFolderButton = $true
if ('%s' -ne '' -and (Test-Path '%s')) {
    $dialog.SelectedPath = '%s'
}
$result = $dialog.ShowDialog($owner)
$owner.Dispose()
if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
    Write-Output $dialog.SelectedPath
}
`, escapedInit, escapedInit, escapedInit)

	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-WindowStyle", "Hidden", "-Command", psScript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", false, fmt.Errorf("folder picker failed: %v (%s)", err, strings.TrimSpace(stderr.String()))
	}

	selected := strings.TrimSpace(stdout.String())
	if selected == "" {
		return "", "", true, nil
	}
	return selected, "", false, nil
}

func RestoreSecurityScopedBookmarkPlatform(bookmarkBase64 string) (string, error) {
	return "", nil
}

func OpenURLPlatform(rawURL string) error {
	return fmt.Errorf("not implemented on windows")
}

func OpenFolderPlatform(absPath string) error {
	return fmt.Errorf("not implemented on windows")
}

func SetAutoStartDarwinMAS(enabled bool) error {
	return nil
}

func ShowAboutDialogPlatform(title, message string) {}
