package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ThumbnailManager handles storage and retrieval of client-uploaded media thumbnails.
// It is completely format-agnostic and relies on mobile clients to generate thumbnails.
type ThumbnailManager struct {
	mu       sync.RWMutex
	cacheDir string
}

// NewThumbnailManager initializes the thumbnail cache directory.
func NewThumbnailManager(baseDir string) (*ThumbnailManager, error) {
	cacheDir := filepath.Join(baseDir, "thumbnails")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create thumbnail directory: %w", err)
	}
	return &ThumbnailManager{
		cacheDir: cacheDir,
	}, nil
}

// GetCacheKey computes a deterministic cache key from a path or hash.
func (tm *ThumbnailManager) GetCacheKey(path string, fileHash string) string {
	if fileHash != "" {
		return fileHash
	}
	h := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path))))
	return hex.EncodeToString(h[:])
}

// GetThumbnail returns cached thumbnail bytes if present.
func (tm *ThumbnailManager) GetThumbnail(key string) ([]byte, string, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Try webp first, then jpg
	for _, ext := range []string{".webp", ".jpg"} {
		filePath := filepath.Join(tm.cacheDir, key+ext)
		if data, err := os.ReadFile(filePath); err == nil && len(data) > 0 {
			mime := "image/jpeg"
			if ext == ".webp" {
				mime = "image/webp"
			}
			return data, mime, true
		}
	}
	return nil, "", false
}

// SaveThumbnail saves thumbnail bytes received from a client.
func (tm *ThumbnailManager) SaveThumbnail(key string, data []byte, mimeType string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	ext := ".jpg"
	if strings.Contains(mimeType, "webp") {
		ext = ".webp"
	}
	filePath := filepath.Join(tm.cacheDir, key+ext)
	return os.WriteFile(filePath, data, 0644)
}

// HasThumbnail checks if a thumbnail exists for the given key.
func (tm *ThumbnailManager) HasThumbnail(key string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, ext := range []string{".webp", ".jpg"} {
		filePath := filepath.Join(tm.cacheDir, key+ext)
		if fi, err := os.Stat(filePath); err == nil && fi.Size() > 0 {
			return true
		}
	}
	return false
}
