package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
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

func TestSetupAppDoesNotLogConfiguredRoleEmails(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}

	t.Setenv("APP_ENV", "production")
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("YTDLP_PATH", executable)
	t.Setenv("HOST_EMAILS", "host-private@example.com")
	t.Setenv("ADMIN_EMAILS", "admin-private@example.com")

	originalWriter := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	defer log.SetOutput(originalWriter)

	if _, _, err := setupApp(); err != nil {
		t.Fatalf("setupApp failed: %v", err)
	}

	for _, privateValue := range []string{
		"host-private@example.com",
		"admin-private@example.com",
	} {
		if strings.Contains(output.String(), privateValue) {
			t.Fatalf("startup log exposed configured role email %q", privateValue)
		}
	}
}
