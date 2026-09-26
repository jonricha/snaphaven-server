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
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ThumbnailManager handles storage, caching, and generation of media thumbnails.
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

// SaveThumbnail saves thumbnail bytes received from a client or generated locally.
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

// GenerateImageThumbnail creates a thumbnail for standard image files (JPEG, PNG, GIF).
func (tm *ThumbnailManager) GenerateImageThumbnail(imagePath string, maxDim int) ([]byte, string, error) {
	f, err := os.Open(imagePath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, "", fmt.Errorf("invalid image dimensions")
	}

	var newW, newH int
	if w > h {
		newW = maxDim
		newH = (h * maxDim) / w
	} else {
		newH = maxDim
		newW = (w * maxDim) / h
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	// Box/nearest-neighbor downsampling
	for y := 0; y < newH; y++ {
		srcY := bounds.Min.Y + (y * h) / newH
		for x := 0; x < newW; x++ {
			srcX := bounds.Min.X + (x * w) / newW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, "", err
	}

	return buf.Bytes(), "image/jpeg", nil
}

// EnsureThumbnail ensures a thumbnail exists for a file, generating one if it's an image.
func (tm *ThumbnailManager) EnsureThumbnail(fullPath string, fileHash string, maxDim int) ([]byte, string, error) {
	key := tm.GetCacheKey(fullPath, fileHash)
	if data, mime, ok := tm.GetThumbnail(key); ok {
		return data, mime, nil
	}

	ext := strings.ToLower(filepath.Ext(fullPath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif":
		data, mime, err := tm.GenerateImageThumbnail(fullPath, maxDim)
		if err != nil {
			return nil, "", err
		}
		_ = tm.SaveThumbnail(key, data, mime)
		return data, mime, nil
	default:
		return nil, "", fmt.Errorf("automatic thumbnail generation unsupported for %s", ext)
	}
}

// Helper to copy data
func copyBytes(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	return buf.Bytes(), err
}
