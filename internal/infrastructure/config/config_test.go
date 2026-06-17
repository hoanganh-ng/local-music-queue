package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set test environment variables
	os.Setenv("PORT", "2222")
	os.Setenv("CLIENT_PIN", "1111")
	os.Setenv("HOST_PIN", "2222")
	os.Setenv("DB_PATH", "./test.db")
	os.Setenv("YTDLP_PATH", "echo") // use 'echo' as a dummy executable that exists

	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("CLIENT_PIN")
		os.Unsetenv("HOST_PIN")
		os.Unsetenv("DB_PATH")
		os.Unsetenv("YTDLP_PATH")
	}()

	cfg := Load()

	if cfg.Port != "2222" {
		t.Errorf("Expected Port 2222, got %s", cfg.Port)
	}
	if cfg.ClientPIN != "1111" {
		t.Errorf("Expected ClientPIN 1111, got %s", cfg.ClientPIN)
	}
	if cfg.HostPIN != "2222" {
		t.Errorf("Expected HostPIN 2222, got %s", cfg.HostPIN)
	}
	if cfg.DBPath != "./test.db" {
		t.Errorf("Expected DBPath ./test.db, got %s", cfg.DBPath)
	}
	if cfg.YTDLPPath != "echo" {
		t.Errorf("Expected YTDLPPath echo, got %s", cfg.YTDLPPath)
	}
}

func TestLoadDefaults(t *testing.T) {
	// Ensure environment variables are not set
	os.Unsetenv("PORT")
	os.Unsetenv("CLIENT_PIN")
	os.Unsetenv("HOST_PIN")
	os.Unsetenv("DB_PATH")
	os.Unsetenv("YTDLP_PATH")

	cfg := Load()

	if cfg.Port != "1111" {
		t.Errorf("Expected default Port 1111, got %s", cfg.Port)
	}
	if cfg.ClientPIN != "5555" {
		t.Errorf("Expected default ClientPIN 5555, got %s", cfg.ClientPIN)
	}
	if cfg.HostPIN != "9512" {
		t.Errorf("Expected default HostPIN 9512, got %s", cfg.HostPIN)
	}
	if cfg.DBPath != "./.localdb/music_queue.db" {
		t.Errorf("Expected default DBPath ./.localdb/music_queue.db, got %s", cfg.DBPath)
	}
	if cfg.YTDLPPath != "yt-dlp" {
		t.Errorf("Expected default YTDLPPath yt-dlp, got %s", cfg.YTDLPPath)
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Run("ValidExecutable", func(t *testing.T) {
		cfg := &Config{YTDLPPath: "ls"} // 'ls' exists on most systems
		if err := cfg.Validate(); err != nil {
			t.Errorf("Expected no error for 'ls', got %v", err)
		}
	})

	t.Run("InvalidExecutable", func(t *testing.T) {
		cfg := &Config{YTDLPPath: "non_existent_binary_hopefully_12345"}
		if err := cfg.Validate(); err == nil {
			t.Error("Expected error for non-existent binary, got nil")
		}
	})
}
