// Package config loads runtime configuration from environment variables.
//
// Sprint 0 wires the minimum set: PORT, POSTGRES_DSN, JWT_SECRET, JWT_ACCESS_TTL_MIN,
// LOG_LEVEL. SMS_STUB_CODE is read by internal/auth (SUB-001). All other envs are
// post-Sprint-0 (multi-instance, HTTPS, real SMS provider).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config groups env-loaded values by domain (network, db, auth, logging, sms)
// rather than by size, because the field count is small and the readability win
// from semantic grouping outweighs the micro-allocation savings fieldalign would buy.
//nolint:govet // semantic grouping > pointer-byte packing (see comment above)
type Config struct {
	JWTSecret      []byte        // pointer-first for alignment (fieldalign hint)
	PostgresDSN    string
	JWTAccessTTL   time.Duration
	Port           string        // :18080 in dev
	SMSStubCode    string        // default "1234" — read by SUB-001
	LogLevel       slog.Level    // debug | info | warn | error
}

// Load reads from process env. Returns an error if any required var is missing
// or malformed. Intended to be called once at startup.
func Load() (*Config, error) {
	cfg := &Config{
		Port:        getEnvDefault("PORT", "18080"),
		LogLevel:    parseLogLevel(getEnvDefault("LOG_LEVEL", "info")),
		SMSStubCode: getEnvDefault("SMS_STUB_CODE", "1234"),
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		return nil, errors.New("config: POSTGRES_DSN is required")
	}
	cfg.PostgresDSN = dsn

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, errors.New("config: JWT_SECRET is required")
	}
	cfg.JWTSecret = []byte(secret)

	ttlMinStr := getEnvDefault("JWT_ACCESS_TTL_MIN", "15")
	ttlMin, err := strconv.Atoi(ttlMinStr)
	if err != nil || ttlMin <= 0 {
		return nil, fmt.Errorf("config: JWT_ACCESS_TTL_MIN must be a positive integer, got %q", ttlMinStr)
	}
	cfg.JWTAccessTTL = time.Duration(ttlMin) * time.Minute

	return cfg, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}