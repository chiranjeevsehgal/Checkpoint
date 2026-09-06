package config

import (
	"testing"
)

func setEnv(t *testing.T, k, v string) {
	t.Helper()
	t.Setenv(k, v)
}

func TestLoadProductionRequiresExplicit(t *testing.T) {
	setEnv(t, "ENV", "production")
	setEnv(t, "DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	setEnv(t, "MINIO_ACCESS_KEY", "ak")
	setEnv(t, "MINIO_SECRET_KEY", "sk")
	setEnv(t, "VAD_BASE_URL", "http://vad:8081")
	setEnv(t, "PORT", "8080")
	if _, err := Load(); err != nil {
		t.Fatalf("valid prod must load: %v", err)
	}

	setEnv(t, "DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing DATABASE_URL in prod must fail")
	}
	setEnv(t, "DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")

	setEnv(t, "VAD_BASE_URL", "http://localhost:8081")
	if _, err := Load(); err == nil {
		t.Fatal("localhost VAD in prod must fail")
	}
	setEnv(t, "VAD_BASE_URL", "http://mock-vad:8081")
	if _, err := Load(); err == nil {
		t.Fatal("mock-vad in prod must fail")
	}
}

func TestLoadInvalidBoolAndPort(t *testing.T) {
	setEnv(t, "ENV", "development")
	setEnv(t, "MINIO_USE_SSL", "banana")
	if _, err := Load(); err == nil {
		t.Fatal("invalid bool must fail")
	}
	setEnv(t, "MINIO_USE_SSL", "true")
	setEnv(t, "PORT", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("invalid PORT must fail")
	}
	setEnv(t, "PORT", "8080")
	setEnv(t, "UPLOAD_EXPIRY_HOURS", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("negative duration must fail")
	}
}
