package config

import (
	"bufio"
	"fmt"
	"log"
	"net/url"
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
	YTDLPPath string

	// DatabaseURL selects the PostgreSQL backend. R03 removed the SQLite
	// fallback; this field must be non-empty in production.
	DatabaseURL string

	// AllowedOrigins is the explicit allow list shared by HTTP CORS and the
	// WebSocket upgrader. Populated from ALLOWED_ORIGINS (comma-separated).
	// When IsLocal is true and the env var is empty, the server substitutes
	// safe loopback defaults at startup. In non-local environments an empty
	// allow list fails Validate().
	AllowedOrigins []string

	// IsLocal mirrors APP_ENV=local. Used by Validate() to decide whether an
	// empty allow list is fatal.
	IsLocal bool
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

	databaseURL := buildDatabaseURL()
	if databaseURL != "" {
		log.Printf("PostgreSQL backend selected (DATABASE_URL host redacted: %s)", RedactDSN(databaseURL))
	} else {
		log.Printf("no DATABASE_URL or POSTGRES_* set; the server will refuse to start until one is provided")
	}

	isLocal := strings.EqualFold(strings.TrimSpace(getEnv("APP_ENV", "local")), "local")

	var allowedOrigins []string
	if raw := strings.TrimSpace(getEnv("ALLOWED_ORIGINS", "")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}

	return &Config{
		Port:           getEnv("PORT", "1111"),
		ClientPIN:      getEnv("CLIENT_PIN", "5555"),
		HostPIN:        getEnv("HOST_PIN", "9512"),
		AdminPIN:       getEnv("ADMIN_PIN", "1598"),
		YTDLPPath:      getEnv("YTDLP_PATH", "yt-dlp"),
		DatabaseURL:    databaseURL,
		IsLocal:        isLocal,
		AllowedOrigins: allowedOrigins,
	}
}

// buildDatabaseURL constructs the PostgreSQL DSN from environment variables.
//
// DATABASE_URL takes precedence. When absent, POSTGRES_HOST/POSTGRES_PORT/
// POSTGRES_USER/POSTGRES_PASSWORD/POSTGRES_DB/POSTGRES_SSLMODE overrides are
// honored. Returns "" when neither is set; the server refuses to start in
// that state (see cmd/server/main.go).
func buildDatabaseURL() string {
	if raw := getEnv("DATABASE_URL", ""); raw != "" {
		return strings.TrimSpace(raw)
	}
	host := getEnv("POSTGRES_HOST", "")
	if host == "" {
		return ""
	}
	user := getEnv("POSTGRES_USER", "")
	password := getEnv("POSTGRES_PASSWORD", "")
	dbname := getEnv("POSTGRES_DB", "")
	port := getEnv("POSTGRES_PORT", "5432")
	sslmode := getEnv("POSTGRES_SSLMODE", "disable")

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   host + ":" + port,
		Path:   "/" + dbname,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
}

// RedactDSN returns a copy of the DSN with the password component replaced
// by "***". It is safe to call on any string; non-DSN strings are returned
// verbatim unless a `password=` token is present, in which case the token's
// value is redacted.
func RedactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	u, err := url.Parse(dsn)
	if err == nil && u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "***")
			return u.String()
		}
		// URL parsed but no password — nothing to redact.
		return dsn
	}
	// Either unparseable or no userinfo — try a raw password= token match.
	return redactRawPassword(dsn)
}

// redactRawPassword replaces the value of the first `password=` token with
// "***". Whitespace or end-of-string ends the token.
func redactRawPassword(s string) string {
	const key = "password="
	idx := strings.Index(s, key)
	if idx < 0 {
		return s
	}
	rest := s[idx+len(key):]
	end := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '\t' || r == '\n' || r == '&' {
			end = i
			break
		}
	}
	return s[:idx+len(key)] + "***" + rest[end:]
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
	if !c.IsLocal && len(c.AllowedOrigins) == 0 {
		return fmt.Errorf("ALLOWED_ORIGINS must be set when APP_ENV != \"local\"")
	}
	return nil
}