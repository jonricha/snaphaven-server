package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestThumbnailManager_SaveAndGet(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "snaphaven_thumb_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tm, err := NewThumbnailManager(tempDir)
	if err != nil {
		t.Fatalf("Failed to create thumbnail manager: %v", err)
	}

	key := "test_hash_123"
	data := []byte("fake_image_bytes")
	mime := "image/jpeg"

	if tm.HasThumbnail(key) {
		t.Errorf("Expected HasThumbnail false before saving")
	}

	if err := tm.SaveThumbnail(key, data, mime); err != nil {
		t.Fatalf("SaveThumbnail failed: %v", err)
	}

	if !tm.HasThumbnail(key) {
		t.Errorf("Expected HasThumbnail true after saving")
	}

	gotData, gotMime, ok := tm.GetThumbnail(key)
	if !ok {
		t.Errorf("Expected GetThumbnail to return true")
	}
	if gotMime != mime {
		t.Errorf("Expected mime %s, got %s", mime, gotMime)
	}
	if !bytes.Equal(gotData, data) {
		t.Errorf("Data mismatch")
	}
}

func TestThumbnailManager_WebPAndJpeg(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "snaphaven_thumb_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tm, err := NewThumbnailManager(tempDir)
	if err != nil {
		t.Fatalf("Failed to create thumbnail manager: %v", err)
	}

	webpKey := "test_webp_456"
	webpData := []byte("RIFFxxxxWEBPVP8 ...")
	if err := tm.SaveThumbnail(webpKey, webpData, "image/webp"); err != nil {
		t.Fatalf("SaveThumbnail failed: %v", err)
	}

	gotData, mime, ok := tm.GetThumbnail(webpKey)
	if !ok || mime != "image/webp" || !bytes.Equal(gotData, webpData) {
		t.Errorf("WebP thumbnail get failed: ok=%v, mime=%s", ok, mime)
	}

	if !tm.HasThumbnail(webpKey) {
		t.Errorf("Expected HasThumbnail true for webpKey")
	}
}

func TestGetAltDCIMPath(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"phone/DCIM/Camera/IMG.jpg", "phone/Camera/IMG.jpg"},
		{"phone/Camera/IMG.jpg", "phone/DCIM/Camera/IMG.jpg"},
		{"DCIM/Camera/IMG.jpg", "Camera/IMG.jpg"},
		{"Camera/IMG.jpg", "DCIM/Camera/IMG.jpg"},
		{"C:/Users/snaphaven/jon/DCIM/Camera/photo.jpg", "C:/Users/snaphaven/jon/Camera/photo.jpg"},
		{"C:/Users/snaphaven/jon/Camera/photo.jpg", "C:/Users/snaphaven/jon/DCIM/Camera/photo.jpg"},
	}

	for _, c := range cases {
		got := filepath.ToSlash(GetAltDCIMPath(c.input))
		if got != c.expected {
			t.Errorf("GetAltDCIMPath(%q) = %q; expected %q", c.input, got, c.expected)
		}
	}
}

func TestThumbnailManager_DCIMAndHashLookup(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "snaphaven_thumb_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tm, err := NewThumbnailManager(tempDir)
	if err != nil {
		t.Fatalf("Failed to create thumbnail manager: %v", err)
	}

	fullPath := "C:\\Users\\snaphaven\\jon\\Camera\\IMG_100.jpg"
	sha := "hash_abc_123"
	thumbData := []byte("thumbnail_data_123")
	mime := "image/jpeg"

	// Save under fullPath key, alt DCIM key, and sha key
	keyPath := tm.GetCacheKey(fullPath, "")
	altKey := tm.GetCacheKey(GetAltDCIMPath(fullPath), "")
	if err := tm.SaveThumbnailWithKeys(thumbData, mime, keyPath, altKey, sha); err != nil {
		t.Fatalf("SaveThumbnailWithKeys failed: %v", err)
	}

	// 1. Should be found by hash
	if !tm.HasThumbnailForFile("", "", sha) {
		t.Errorf("Expected HasThumbnailForFile true by hash")
	}

	// 2. Should be found by exact path
	if !tm.HasThumbnailForFile(fullPath, "jon\\Camera\\IMG_100.jpg", "") {
		t.Errorf("Expected HasThumbnailForFile true by exact path")
	}

	// 3. Should be found by DCIM alternative path!
	dcimPath := "C:\\Users\\snaphaven\\jon\\DCIM\\Camera\\IMG_100.jpg"
	if !tm.HasThumbnailForFile(dcimPath, "jon\\DCIM\\Camera\\IMG_100.jpg", "") {
		t.Errorf("Expected HasThumbnailForFile true by DCIM alternative path")
	}

	// 4. Retrieve by DCIM alternative path
	data, _, ok := tm.GetThumbnailForFile(dcimPath, "jon\\DCIM\\Camera\\IMG_100.jpg", "")
	if !ok || !bytes.Equal(data, thumbData) {
		t.Errorf("GetThumbnailForFile by DCIM path failed")
	}
}

func TestThumbnailManager_GenerateLocalThumbnail(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "snaphaven_thumb_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tm, err := NewThumbnailManager(tempDir)
	if err != nil {
		t.Fatalf("Failed to create thumbnail manager: %v", err)
	}

	// Create a dummy JPEG image on disk
	imgDir := filepath.Join(tempDir, "photos")
	_ = os.MkdirAll(imgDir, 0755)
	imgPath := filepath.Join(imgDir, "test.jpg")

	// 400x300 image
	src := image.NewRGBA(image.Rect(0, 0, 400, 300))
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	if err := jpeg.Encode(f, src, &jpeg.Options{Quality: 80}); err != nil {
		f.Close()
		t.Fatalf("Failed to encode test image: %v", err)
	}
	f.Close()

	// Generate thumbnail
	data, mime, ok := tm.GenerateLocalThumbnail(imgPath)
	if !ok {
		t.Fatalf("GenerateLocalThumbnail failed")
	}
	if mime != "image/jpeg" {
		t.Errorf("Expected mime image/jpeg, got %s", mime)
	}
	if len(data) == 0 {
		t.Errorf("Expected non-empty thumbnail data")
	}

	// Next lookup should hit cache
	if !tm.HasThumbnailForFile(imgPath, "", "") {
		t.Errorf("Expected HasThumbnailForFile true after generation")
	}
}


