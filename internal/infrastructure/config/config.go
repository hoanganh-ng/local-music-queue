package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// Config holds the application configuration.
type Config struct {
	Port      string
	ClientPIN string
	HostPIN   string
	AdminPIN  string
	DBPath    string
	YTDLPPath string
}

// Load loads the configuration from environment variables with defaults.
func Load() *Config {
	if getEnv("APP_ENV", "local") == "local" {
		log.Println("Running in local environment, using default configuration")
		dotenvPath := ".env"
		if _, err := os.Stat(dotenvPath); os.IsNotExist(err) {
			log.Printf("Warning: %s file not found, using environment variables or defaults", dotenvPath)
		} else {
			if err := loadDotEnv(dotenvPath); err != nil {
				log.Printf("Error loading .env file: %v", err)
			} else {
				log.Printf("Loaded configuration from %s", dotenvPath)
			}
		}
	}

	return &Config{
		Port:      getEnv("PORT", "1111"),
		ClientPIN: getEnv("CLIENT_PIN", "5555"),
		HostPIN:   getEnv("HOST_PIN", "9512"),
		AdminPIN:  getEnv("ADMIN_PIN", "1598"),
		DBPath:    getEnv("DB_PATH", "./.localdb/music_queue.db"),
		YTDLPPath: getEnv("YTDLP_PATH", "yt-dlp"),
	}
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			log.Printf("Warning: Invalid line in .env file: %s", line)
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		os.Setenv(key, value)
	}

	return scanner.Err()
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
