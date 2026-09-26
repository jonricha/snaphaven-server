package main

import (
	"bytes"
	"image"
	"image/color"
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

func TestThumbnailManager_GenerateImageThumbnail(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "snaphaven_thumb_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tm, err := NewThumbnailManager(tempDir)
	if err != nil {
		t.Fatalf("Failed to create thumbnail manager: %v", err)
	}

	// Create a dummy JPEG image
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	testImgPath := filepath.Join(tempDir, "sample.jpg")
	f, err := os.Create(testImgPath)
	if err != nil {
		t.Fatalf("Failed to create sample image: %v", err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		f.Close()
		t.Fatalf("Failed to encode sample image: %v", err)
	}
	f.Close()

	thumbData, mime, err := tm.GenerateImageThumbnail(testImgPath, 300)
	if err != nil {
		t.Fatalf("GenerateImageThumbnail failed: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("Expected mime image/jpeg, got %s", mime)
	}

	// Verify decoded thumbnail dimensions
	thumbImg, err := jpeg.Decode(bytes.NewReader(thumbData))
	if err != nil {
		t.Fatalf("Failed to decode generated thumbnail: %v", err)
	}
	bounds := thumbImg.Bounds()
	if bounds.Dx() != 300 {
		t.Errorf("Expected width 300, got %d", bounds.Dx())
	}
	if bounds.Dy() != 225 {
		t.Errorf("Expected height 225, got %d", bounds.Dy())
	}

	// Test EnsureThumbnail
	ensured, _, err := tm.EnsureThumbnail(testImgPath, "sample_hash", 300)
	if err != nil {
		t.Fatalf("EnsureThumbnail failed: %v", err)
	}
	if len(ensured) == 0 {
		t.Errorf("Expected non-empty ensured thumbnail")
	}
}
