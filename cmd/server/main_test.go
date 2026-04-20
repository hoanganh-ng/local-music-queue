package main

import (
	"os"
	"testing"
)

func TestSetupApp(t *testing.T) {
	// Create a temporary directory for the database
	tmpDir, err := os.MkdirTemp("", "music-queue-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := tmpDir + "/test.db"

	// Set environment variables for the test
	os.Setenv("DB_PATH", dbPath)
	os.Setenv("YTDLP_PATH", "/home/vi0l3tsc0rpi0n/linux-softwares/yt-dlp")
	defer func() {
		os.Unsetenv("DB_PATH")
		os.Unsetenv("YTDLP_PATH")
	}()

	mux, cfg, err := setupApp()
	if err != nil {
		t.Fatalf("setupApp failed: %v", err)
	}

	if mux == nil {
		t.Fatal("Expected mux to be non-nil")
	}

	if cfg.DBPath != dbPath {
		t.Errorf("Expected DBPath %s, got %s", dbPath, cfg.DBPath)
	}
}
