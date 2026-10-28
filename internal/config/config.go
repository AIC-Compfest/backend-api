package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv                 string
	Port                   string
	LogLevel               string
	SupabaseURL            string
	SupabasePublishableKey string
	SupabaseServiceRoleKey string
	AIServiceURL           string
	JWTSecret              string
	SchemaVersion          string
	ConfigVersion          string
	WorkerPollInterval     time.Duration
	AIServiceTimeout       time.Duration
}

func loadDotEnv() {
	paths := []string{".env", "../.env", "../../.env"}
	for _, p := range paths {
		content, err := os.ReadFile(p)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					k := strings.TrimSpace(parts[0])
					v := strings.TrimSpace(parts[1])
					v = strings.Trim(v, `"'`)
					if os.Getenv(k) == "" {
						os.Setenv(k, v)
					}
				}
			}
			break
		}
	}
}

func envOrSecret(envName, secretPath string) string {
	if value := os.Getenv(envName); value != "" {
		return value
	}

	data, err := os.ReadFile(secretPath)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func Load() (Config, error) {
	loadDotEnv()

	pollSeconds, err := strconv.Atoi(value("WORKER_POLL_INTERVAL_SECONDS", "5"))
	if err != nil || pollSeconds < 1 {
		return Config{}, errors.New("WORKER_POLL_INTERVAL_SECONDS must be a positive integer")
	}
	aiTimeoutSeconds, err := strconv.Atoi(value("AI_SERVICE_TIMEOUT_SECONDS", "120"))
	if err != nil || aiTimeoutSeconds < 1 {
		return Config{}, errors.New("AI_SERVICE_TIMEOUT_SECONDS must be a positive integer")
	}
	cfg := Config{
		AppEnv:                 value("APP_ENV", "development"),
		Port:                   value("PORT", "8080"),
		LogLevel:               value("LOG_LEVEL", "INFO"),
		SupabaseURL:            strings.TrimRight(value("SUPABASE_URL", ""), "/"),
		SupabasePublishableKey: value("SUPABASE_PUBLISHABLE_KEY", ""),
		SupabaseServiceRoleKey: envOrSecret("SUPABASE_SERVICE_ROLE_KEY", "/run/secrets/SUPABASE_SERVICE_ROLE_KEY"),
		AIServiceURL:           strings.TrimRight(value("AI_SERVICE_URL", ""), "/"),
		JWTSecret:              envOrSecret("JWT_SECRET", "/run/secrets/JWT_SECRET"),
		SchemaVersion:          value("SCHEMA_VERSION", "0.1.0"),
		ConfigVersion:          value("CONFIG_VERSION", "0.1.0"),
		WorkerPollInterval:     time.Duration(pollSeconds) * time.Second,
		AIServiceTimeout:       time.Duration(aiTimeoutSeconds) * time.Second,
	}
	if cfg.AIServiceURL == "" {
		return Config{}, errors.New("AI_SERVICE_URL is required")
	}
	if _, err := url.ParseRequestURI(cfg.AIServiceURL); err != nil {
		return Config{}, fmt.Errorf("AI_SERVICE_URL: %w", err)
	}
	if err := cfg.ValidateSupabase(); err != nil {
		return Config{}, err
	}
	if cfg.SupabaseServiceRoleKey == "" {
		return Config{}, errors.New("SUPABASE_SERVICE_ROLE_KEY is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET must be set and contain at least 32 characters")
	}
	return cfg, nil
}

func (c Config) ValidateSupabase() error {
	if c.SupabaseURL == "" {
		return errors.New("SUPABASE_URL is required")
	}
	if _, err := url.ParseRequestURI(c.SupabaseURL); err != nil {
		return fmt.Errorf("SUPABASE_URL: %w", err)
	}
	if c.SupabasePublishableKey == "" {
		return errors.New("SUPABASE_PUBLISHABLE_KEY is required")
	}
	return nil
}

func value(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
