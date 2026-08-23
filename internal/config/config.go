package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config holds the complete runtime configuration for pgpool.
// Every value has a sensible default - a user with a minimal
// config file gets a working pooler out of the box.
type Config struct {
	Server  ServerConfig  `json:"server"`
	Backend BackendConfig `json:"backend"`
	Pool    PoolConfig    `json:"pool"`
	Metrics MetricsConfig `json:"metrics"`
}

// ServerConfig defines where pgpool listens for client connections.
type ServerConfig struct {
	// Host is the address pgpool binds to.
	// "0.0.0.0" means all interfaces - corrrect for a server.
	Host string `json:"host"`

	// Port is the port pgpool listens on.
	// 5433 by convention - one above Postgres's 5432
	Port int `json:"port"`
}

// BackendConfig defines how pgpool connects to Postgres.
type BackendConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// PoolConfig controls connection pool behaviour.
type PoolConfig struct {
	// Size is the number of real backend connections to maintain.
	// Formula: (core_count * 2) + effective_spindle_count.
	// Default of 20 suits most deployments.
	Size int `json:"size"`

	// Mode controls when backend connections are returned to the pool.
	// Valid values: "session", "transaction", "statement"
	Mode string `json:"mode"`

	// QueueTimeout is how long a client waits for a backend
	// connection before being rejected.
	QueueTimeout duration `json:"queue_timeout"`

	// QueueMaxDepth is the maximum number of clients that can
	// wait for a backend connection simultaneously.
	// When full, new clients are rejected immediately.
	QueueMaxDepth int `json:"queue_max_depth"`

	// ResetQuery runs every time a backend connection is returned
	// to the pool in transaction or statement mode.
	// DISCARD ALL resets all session state - safe default.
	ResetQuery string `json:"reset_query"`

	// HealthCheckInterval controls how often pgpool checks that
	// backend connections are still alive.
	HealthCheckInterval duration `json:"health_check_interval"`
}

// MetricsConfig controls Prometheus metrics exposure.
type MetricsConfig struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

// duration is a wrapper around time.Duration that supports
// JSON unmarshalling from a human-readable string like "30s".
// time.Duration does not support this by default - it expects
// a raw integer (nanoseconds), which is unreadable in a config file.
type duration struct {
	time.Duration
}

func (d *duration) UnmarshalJSON(b []byte) error {
	// JSON gives us a quoted string like "30s".
	// Trim the quotes before parsing.
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("parsing duration: %w", err)
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	d.Duration = parsed
	return nil
}

// Default returns a Config with production-safe defaults.
// Every field is set - nothing is a zero value surprise.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 5433,
		},
		Backend: BackendConfig{
			Host: "localhost",
			Port: 5432,
		},
		Pool: PoolConfig{
			Size:                20,
			Mode:                "transaction",
			QueueTimeout:        duration{30 * time.Second},
			ResetQuery:          "DISCARD ALL",
			HealthCheckInterval: duration{10 * time.Second},
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Port:    9090,
		},
	}
}

// Load reads a JSON config file from path and merges it on top
// of the defaults. Fields not present in the file keep their
// default values.
func Load(path string) (Config, error) {
	// Start with defaults so missing fields are never zero values.
	cfg := Default()

	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("opening config file: %w", err)
	}
	defer f.Close()

	// json.Decoder reads from a stream - no need to read the
	// entire file into memory first.
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decoding config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("validation config: %w", err)
	}

	return cfg, nil
}

// validate checks that the config is internally consistent.
// We validate at startup so that program never runs with a
// broken config - fail fast, fail loudly.
func (c Config) validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", c.Server.Port)
	}

	if c.Backend.Port < 1 || c.Backend.Port > 65535 {
		return fmt.Errorf("backend.port must be between 1 and 65535, got %d", c.Server.Port)
	}

	if c.Backend.Host == "" {
		return fmt.Errorf("backend.host is required")
	}

	if c.Backend.Database == "" {
		return fmt.Errorf("backend.database is requrired")
	}

	if c.Backend.User == "" {
		return fmt.Errorf("backend.user is required")
	}

	if c.Pool.Size < 1 {
		return fmt.Errorf("pool.size must be at least 1, got %d", c.Pool.Size)
	}

	if c.Pool.QueueMaxDepth < 0 {
		return fmt.Errorf("pool.queue_max_depth must be non-negative, got %d", c.Pool.QueueMaxDepth)
	}

	switch c.Pool.Mode {
	case "session", "transaction", "statement":
		// valid
	default:
		return fmt.Errorf("pool.mode must be session, transaction, or satement, got %q", c.Pool.Mode)
	}

	return nil
}

// Addr returns the full listen address for the server.
func (c ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// DSN returns a Postgres connection string for the backend.
func (c BackendConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		c.Host, c.Port, c.Database, c.User, c.Password)
}
