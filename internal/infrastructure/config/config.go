package config

import (
	"fmt"
	"os"
	"os/exec"
)

// Config holds the application configuration.
type Config struct {
	Port      string
	ClientPIN string
	HostPIN   string
	DBPath    string
	YTDLPPath string
}

// Load loads the configuration from environment variables with defaults.
func Load() *Config {
	return &Config{
		Port:      getEnv("PORT", "1111"),
		ClientPIN: getEnv("CLIENT_PIN", "5555"),
		HostPIN:   getEnv("HOST_PIN", "6666"),
		DBPath:    getEnv("DB_PATH", "./.localdb/music_queue.db"),
		YTDLPPath: getEnv("YTDLP_PATH", "yt-dlp"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if _, err := exec.LookPath(c.YTDLPPath); err != nil {
		return fmt.Errorf("yt-dlp executable not found at %s: %w", c.YTDLPPath, err)
	}
	return nil
}
