// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration for the backend service.
type Config struct {
	BackendPort string

	PostgresHost     string
	PostgresPort     string
	PostgresDB       string
	PostgresUser     string
	PostgresPassword string

	RedisHost string
	RedisPort string

	RedpandaBrokers string

	AIServiceURL     string
	AIRequestTimeout time.Duration

	// PaymentProvider selects the ExecutionEngine's PaymentProvider
	// implementation: "fake" (default, safe for local/dev/demo — no
	// external network calls) or "razorpay" (requires RazorpayKeyID and
	// RazorpayKeySecret; see service.NewRazorpayProvider).
	PaymentProvider   string
	RazorpayKeyID     string
	RazorpayKeySecret string
	RazorpayBaseURL   string
}

// Load reads configuration from environment variables, applying sensible
// local-development defaults for anything left unset.
func Load() Config {
	return Config{
		BackendPort: getEnv("BACKEND_PORT", "8080"),

		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresDB:       getEnv("POSTGRES_DB", "revguard"),
		PostgresUser:     getEnv("POSTGRES_USER", "revguard"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "revguard"),

		RedisHost: getEnv("REDIS_HOST", "localhost"),
		RedisPort: getEnv("REDIS_PORT", "6379"),

		RedpandaBrokers: getEnv("REDPANDA_BROKERS", "localhost:9092"),

		AIServiceURL:     getEnv("AI_SERVICE_URL", "http://localhost:8000"),
		AIRequestTimeout: getEnvSeconds("AI_REQUEST_TIMEOUT_SECONDS", 20),

		PaymentProvider:   getEnv("PAYMENT_PROVIDER", "fake"),
		RazorpayKeyID:     getEnv("RAZORPAY_KEY_ID", ""),
		RazorpayKeySecret: getEnv("RAZORPAY_KEY_SECRET", ""),
		RazorpayBaseURL:   getEnv("RAZORPAY_BASE_URL", ""),
	}
}

// PostgresDSN builds a libpq-style connection string from the config.
func (c Config) PostgresDSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.PostgresUser, c.PostgresPassword, c.PostgresHost, c.PostgresPort, c.PostgresDB,
	)
}

// RedisAddr builds a host:port address for the Redis client.
func (c Config) RedisAddr() string {
	return fmt.Sprintf("%s:%s", c.RedisHost, c.RedisPort)
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvSeconds(key string, fallbackSeconds int) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return time.Duration(fallbackSeconds) * time.Second
}
