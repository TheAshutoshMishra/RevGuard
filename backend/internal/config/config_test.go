package config

import "testing"

func TestPostgresDSN_BuildsFromIndividualFieldsByDefault(t *testing.T) {
	c := Config{
		PostgresUser: "u", PostgresPassword: "p", PostgresHost: "h",
		PostgresPort: "5432", PostgresDB: "d", PostgresSSLMode: "disable",
	}
	got := c.PostgresDSN()
	want := "postgres://u:p@h:5432/d?sslmode=disable"
	if got != want {
		t.Fatalf("PostgresDSN() = %q, want %q", got, want)
	}
}

func TestPostgresDSN_HonorsConfiguredSSLMode(t *testing.T) {
	c := Config{
		PostgresUser: "u", PostgresPassword: "p", PostgresHost: "h",
		PostgresPort: "5432", PostgresDB: "d", PostgresSSLMode: "require",
	}
	got := c.PostgresDSN()
	want := "postgres://u:p@h:5432/d?sslmode=require"
	if got != want {
		t.Fatalf("PostgresDSN() = %q, want %q", got, want)
	}
}

func TestPostgresDSN_DatabaseURLTakesPrecedence(t *testing.T) {
	c := Config{
		DatabaseURL:  "postgres://managed-host/db?sslmode=require",
		PostgresUser: "u", PostgresPassword: "p", PostgresHost: "h",
		PostgresPort: "5432", PostgresDB: "d", PostgresSSLMode: "disable",
	}
	got := c.PostgresDSN()
	want := "postgres://managed-host/db?sslmode=require"
	if got != want {
		t.Fatalf("PostgresDSN() = %q, want %q (DatabaseURL should override individual fields)", got, want)
	}
}

func TestLoad_DefaultsPostgresSSLModeToDisable(t *testing.T) {
	c := Load()
	if c.PostgresSSLMode != "disable" {
		t.Fatalf("Load().PostgresSSLMode = %q, want %q (must not change existing local-dev behavior)", c.PostgresSSLMode, "disable")
	}
	if c.DatabaseURL != "" {
		t.Fatalf("Load().DatabaseURL = %q, want empty when DATABASE_URL is unset", c.DatabaseURL)
	}
}
