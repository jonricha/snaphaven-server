package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ThumbnailManager handles storage and retrieval of client-uploaded media thumbnails,
// with on-demand fallback generation for standard desktop media.
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

// GetAltDCIMPath returns an alternative path toggling DCIM folder (e.g. phone/DCIM/Camera <-> phone/Camera).
// Handles both forward and backward slashes uniformly across OS platforms.
func GetAltDCIMPath(path string) string {
	if path == "" {
		return ""
	}
	hasBackslash := strings.Contains(path, "\\")
	slash := strings.ReplaceAll(path, "\\", "/")

	var alt string
	if strings.Contains(slash, "/DCIM/") {
		alt = strings.Replace(slash, "/DCIM/", "/", 1)
	} else if strings.HasPrefix(slash, "DCIM/") {
		alt = strings.TrimPrefix(slash, "DCIM/")
	} else if strings.Contains(slash, "/Camera/") {
		alt = strings.Replace(slash, "/Camera/", "/DCIM/Camera/", 1)
	} else if strings.HasPrefix(slash, "Camera/") {
		alt = "DCIM/" + slash
	}

	if alt == "" {
		return ""
	}

	if hasBackslash {
		return strings.ReplaceAll(alt, "/", "\\")
	}
	return alt
}

// GetThumbnail returns cached thumbnail bytes if present.
func (tm *ThumbnailManager) GetThumbnail(key string) ([]byte, string, bool) {
	if key == "" {
		return nil, "", false
	}
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
	if key == "" || len(data) == 0 {
		return nil
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()

	ext := ".jpg"
	if strings.Contains(mimeType, "webp") {
		ext = ".webp"
	}
	filePath := filepath.Join(tm.cacheDir, key+ext)
	return os.WriteFile(filePath, data, 0644)
}

// SaveThumbnailWithKeys saves the thumbnail under primaryKey and any additional keys (e.g. hash and alt paths).
func (tm *ThumbnailManager) SaveThumbnailWithKeys(data []byte, mimeType string, keys ...string) error {
	var firstErr error
	for _, key := range keys {
		if key == "" {
			continue
		}
		if err := tm.SaveThumbnail(key, data, mimeType); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// HasThumbnail checks if a thumbnail exists for the given key.
func (tm *ThumbnailManager) HasThumbnail(key string) bool {
	if key == "" {
		return false
	}
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

// HasThumbnailForFile checks if a thumbnail exists for a file by hash, fullPath, alt DCIM path, or cleanPath.
func (tm *ThumbnailManager) HasThumbnailForFile(fullPath string, cleanPath string, fileHash string) bool {
	if fileHash != "" && tm.HasThumbnail(fileHash) {
		return true
	}
	if fullPath != "" {
		if tm.HasThumbnail(tm.GetCacheKey(fullPath, "")) {
			return true
		}
		if alt := GetAltDCIMPath(fullPath); alt != "" && tm.HasThumbnail(tm.GetCacheKey(alt, "")) {
			return true
		}
	}
	if cleanPath != "" {
		if tm.HasThumbnail(tm.GetCacheKey(cleanPath, "")) {
			return true
		}
		if alt := GetAltDCIMPath(cleanPath); alt != "" && tm.HasThumbnail(tm.GetCacheKey(alt, "")) {
			return true
		}
	}
	return false
}

// GetThumbnailForFile retrieves a thumbnail checking fileHash, fullPath, alt DCIM path, and cleanPath.
func (tm *ThumbnailManager) GetThumbnailForFile(fullPath string, cleanPath string, fileHash string) ([]byte, string, bool) {
	if fileHash != "" {
		if data, mime, ok := tm.GetThumbnail(fileHash); ok {
			return data, mime, true
		}
	}
	if fullPath != "" {
		if data, mime, ok := tm.GetThumbnail(tm.GetCacheKey(fullPath, "")); ok {
			return data, mime, true
		}
		if alt := GetAltDCIMPath(fullPath); alt != "" {
			if data, mime, ok := tm.GetThumbnail(tm.GetCacheKey(alt, "")); ok {
				return data, mime, true
			}
		}
	}
	if cleanPath != "" {
		if data, mime, ok := tm.GetThumbnail(tm.GetCacheKey(cleanPath, "")); ok {
			return data, mime, true
		}
		if alt := GetAltDCIMPath(cleanPath); alt != "" {
			if data, mime, ok := tm.GetThumbnail(tm.GetCacheKey(alt, "")); ok {
				return data, mime, true
			}
		}
	}
	return nil, "", false
}

// GenerateLocalThumbnail generates a thumbnail on-the-fly for supported image files (.jpg, .jpeg, .png, .gif) on the local disk.
func (tm *ThumbnailManager) GenerateLocalThumbnail(fullPath string) ([]byte, string, bool) {
	if fullPath == "" {
		return nil, "", false
	}

	fi, err := os.Stat(fullPath)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return nil, "", false
	}

	ext := strings.ToLower(filepath.Ext(fullPath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif":
		// Supported standard desktop formats
	default:
		// Unsupported for server-side generation (e.g. HEIC, MP4, MOV)
		return nil, "", false
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return nil, "", false
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, "", false
	}

	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, "", false
	}

	maxDim := 300
	newW, newH := w, h
	if w > h {
		if w > maxDim {
			newW = maxDim
			newH = (h * maxDim) / w
		}
	} else {
		if h > maxDim {
			newH = maxDim
			newW = (w * maxDim) / h
		}
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := bounds.Min.Y + (y*h)/newH
		for x := 0; x < newW; x++ {
			srcX := bounds.Min.X + (x*w)/newW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, "", false
	}
	thumbBytes := buf.Bytes()
	mime := "image/jpeg"

	// Cache under fullPath key and alt DCIM key
	primaryKey := tm.GetCacheKey(fullPath, "")
	var altKey string
	if alt := GetAltDCIMPath(fullPath); alt != "" {
		altKey = tm.GetCacheKey(alt, "")
	}
	_ = tm.SaveThumbnailWithKeys(thumbBytes, mime, primaryKey, altKey)

	return thumbBytes, mime, true
}

