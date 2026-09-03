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

	// PolicyProfile selects which named service.PolicyConfig
	// (conservative/balanced/aggressive — see service.PolicyProfiles,
	// Milestone 10) PolicyEngine evaluates against. Defaults to
	// "balanced", which is numerically identical to the original
	// Milestone 5 DefaultPolicyConfig — an unset POLICY_PROFILE changes
	// nothing about existing production behavior.
	PolicyProfile string

	// PaymentProvider selects the ExecutionEngine's PaymentProvider
	// implementation: "fake" (default, safe for local/dev/demo — no
	// external network calls) or "razorpay" (requires RazorpayKeyID and
	// RazorpayKeySecret; see service.NewRazorpayProvider).
	PaymentProvider   string
	RazorpayKeyID     string
	RazorpayKeySecret string
	RazorpayBaseURL   string

	// RazorpayWebhookSecret authenticates inbound Razorpay webhooks (see
	// service.NewConfiguredWebhookVerifier). Left empty, every webhook is
	// rejected — verification never fails open.
	RazorpayWebhookSecret string

	// FakeReconcilerScenario/Amount/Currency configure the
	// PaymentReconciler used when PaymentProvider is "fake" (see
	// cmd/server/main.go's buildPaymentReconciler) — local/dev/test only,
	// mirroring FakeProvider's determinism. Ignored when PaymentProvider
	// is "razorpay".
	FakeReconcilerScenario string
	FakeReconcilerAmount   int64
	FakeReconcilerCurrency string
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

		PolicyProfile: getEnv("POLICY_PROFILE", "balanced"),

		PaymentProvider:   getEnv("PAYMENT_PROVIDER", "fake"),
		RazorpayKeyID:     getEnv("RAZORPAY_KEY_ID", ""),
		RazorpayKeySecret: getEnv("RAZORPAY_KEY_SECRET", ""),
		RazorpayBaseURL:   getEnv("RAZORPAY_BASE_URL", ""),

		RazorpayWebhookSecret: getEnv("RAZORPAY_WEBHOOK_SECRET", ""),

		FakeReconcilerScenario: getEnv("RECONCILER_FAKE_SCENARIO", "payment_captured"),
		FakeReconcilerAmount:   getEnvInt64("RECONCILER_FAKE_AMOUNT_MINOR_UNITS", 0),
		FakeReconcilerCurrency: getEnv("RECONCILER_FAKE_CURRENCY", "INR"),
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

func getEnvInt64(key string, fallback int64) int64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}
