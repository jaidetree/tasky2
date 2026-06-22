// Package config reads Tasky's runtime configuration from the environment.
package config

import (
	"os"
	"strconv"
)

// Config is the resolved runtime configuration. DB connection is not held here:
// it comes from the standard PG* libpq variables consumed directly by pgx.
type Config struct {
	// Host is the bind address. Default 127.0.0.1 keeps the app loopback-only;
	// it listens on a network only when TASKY_HOST is set (ADR-0001).
	Host string
	// Port is the HTTP port. Default 8080.
	Port string
	// MaxInProgress is the concurrency limit (default 1, scales to 3).
	MaxInProgress int
	// PublicDir is the directory of built frontend files served in production
	// (ADR-0005). Empty disables static serving (e.g. dev, where Vite serves).
	PublicDir string
}

// Load reads configuration from the environment, applying defaults.
func Load() Config {
	return Config{
		Host:          getenv("TASKY_HOST", "127.0.0.1"),
		Port:          getenv("TASKY_PORT", "8080"),
		MaxInProgress: getenvInt("TASKY_MAX_IN_PROGRESS", 1),
		PublicDir:     getenv("TASKY_PUBLIC_DIR", "public"),
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
