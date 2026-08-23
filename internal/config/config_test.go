package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/sanverite/pgpool/internal/config"
)

func TestDefault(t *testing.T) {
	cfg := config.Default()

	// Verify defaults are sensible — not zero values.
	if cfg.Server.Port != 5433 {
		t.Errorf("expected server port 5433, got %d", cfg.Server.Port)
	}

	if cfg.Pool.Size != 20 {
		t.Errorf("expected pool size 20, got %d", cfg.Pool.Size)
	}

	if cfg.Pool.Mode != "transaction" {
		t.Errorf("expected pool mode 'transaction', got %q", cfg.Pool.Mode)
	}

	if cfg.Pool.QueueTimeout.Duration != 30*time.Second {
		t.Errorf("expected queue timeout 30s, got %v", cfg.Pool.QueueTimeout.Duration)
	}
}

func TestLoadValidConfig(t *testing.T) {
	// Write a minimal config file to a temp file.
	// We only set what we want to override — defaults fill the rest.
	content := `{
		"backend": {
			"host": "mypostgres",
			"port": 5432,
			"database": "mydb",
			"user":     "myuser",
			"password": "secret"
		}
	}`

	f := writeTempFile(t, content)

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Backend fields should be overridden.
	if cfg.Backend.Host != "mypostgres" {
		t.Errorf("expected backend host 'mypostgres', got %q", cfg.Backend.Host)
	}

	// Pool size should still be the default — not in the file.
	if cfg.Pool.Size != 20 {
		t.Errorf("expected default pool size 20, got %d", cfg.Pool.Size)
	}
}

func TestLoadInvalidMode(t *testing.T) {
	content := `{
		"backend": {
			"host": "localhost",
			"port": 5432,
			"database": "mydb",
			"user": "myuser",
			"password": "secret"
		},
		"pool": {
			"mode": "invalid"
		}
	}`

	f := writeTempFile(t, content)

	_, err := config.Load(f)
	if err == nil {
		t.Fatal("expected error for invalid pool mode, got nil")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/pgpool.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// writeTempFile writes content to a temp file and returns the path.
// The file is automatically deleted when the test ends.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()

	f, err := os.CreateTemp("", "pgpool-config-*.json")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}

	// Register cleanup — temp file is deleted when test ends.
	// This is the correct pattern — no manual defer os.Remove needed.
	t.Cleanup(func() { os.Remove(f.Name()) })

	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("closing temp file: %v", err)
	}

	return f.Name()
}
