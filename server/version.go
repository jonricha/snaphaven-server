package main

import (
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
)

// Embedded build variables set via -ldflags during compilation
var (
	Version   = "v0.0.0-dev"
	Commit    = "none"
	BuildTime = "unknown"
)

func init() {
	// Fallback to Go's embedded VCS build info if not injected via -ldflags
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if (Commit == "" || Commit == "none") && setting.Key == "vcs.revision" {
				Commit = setting.Value
			}
			if (BuildTime == "" || BuildTime == "unknown") && setting.Key == "vcs.time" {
				BuildTime = setting.Value
			}
		}
	}
}

// GetVersion returns the current version string
func GetVersion() string {
	if Version == "" {
		return "v0.0.0-dev"
	}
	return Version
}

// GetCommit returns the git commit hash
func GetCommit() string {
	return Commit
}

// GetBuildTime returns the build timestamp
func GetBuildTime() string {
	return BuildTime
}

// GetFormattedVersion returns a user-friendly version string
func GetFormattedVersion() string {
	v := GetVersion()
	if Commit != "" && Commit != "none" {
		if len(Commit) > 7 {
			v += fmt.Sprintf(" (%s)", Commit[:7])
		} else {
			v += fmt.Sprintf(" (%s)", Commit)
		}
	}
	return v
}

// isNewerVersion returns true if remoteVer is strictly greater than currentVer using semantic versioning rules.
// Example: isNewerVersion("v1.0.1", "v1.0.0") -> true
func isNewerVersion(remoteVer, currentVer string) bool {
	rMajor, rMinor, rPatch := parseSemver(remoteVer)
	cMajor, cMinor, cPatch := parseSemver(currentVer)

	if rMajor != cMajor {
		return rMajor > cMajor
	}
	if rMinor != cMinor {
		return rMinor > cMinor
	}
	return rPatch > cPatch
}

// parseSemver extracts major, minor, patch numbers from a version string like "v1.2.3" or "1.2.3-beta"
func parseSemver(ver string) (int, int, int) {
	ver = strings.TrimSpace(ver)
	ver = strings.TrimPrefix(ver, "v")
	ver = strings.TrimPrefix(ver, "V")

	// Cut off pre-release or build metadata (e.g. -alpha, +build)
	if idx := strings.IndexAny(ver, "-+"); idx != -1 {
		ver = ver[:idx]
	}

	parts := strings.Split(ver, ".")
	major, minor, patch := 0, 0, 0

	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}

	return major, minor, patch
}
