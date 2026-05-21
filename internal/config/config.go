package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	SupabaseURL            string
	SupabaseServiceRoleKey string

	ServerName        string
	ServerURL         string
	ServerRegion      string
	ServerPriority    int
	ServerMaxCapacity int

	HTTPListen     string
	InternalAPIKey string

	StoreDBPath string

	LogLevel string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	c := &Config{
		SupabaseURL:            os.Getenv("SUPABASE_URL"),
		SupabaseServiceRoleKey: os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),

		ServerName:        envOr("SERVER_NAME", "VPS hallowa-server1"),
		ServerURL:         envOr("SERVER_URL", "http://localhost:3000"),
		ServerRegion:      envOr("SERVER_REGION", "ID"),
		ServerPriority:    envInt("SERVER_PRIORITY", 100),
		ServerMaxCapacity: envInt("SERVER_MAX_CAPACITY", 50),

		HTTPListen:     envOr("HTTP_LISTEN", ":3000"),
		InternalAPIKey: os.Getenv("INTERNAL_API_KEY"),

		StoreDBPath: envOr("STORE_DB_PATH", "/home/zesbe/.local/share/hallowa-be/store.db"),

		LogLevel: envOr("LOG_LEVEL", "info"),
	}

	if c.SupabaseURL == "" {
		return nil, fmt.Errorf("SUPABASE_URL is required")
	}
	if c.SupabaseServiceRoleKey == "" {
		return nil, fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY is required")
	}

	return c, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
