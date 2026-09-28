package main

import (
	"bytes"
	"os"
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

