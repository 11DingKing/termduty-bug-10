// Package config parses service configuration from a YAML file with environment
// variable overrides. The HTTP port, data directory and timeouts can all be set
// either way; the default port is 57615.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the resolved service configuration.
type Config struct {
	HTTPAddr        string        `yaml:"http_addr"`
	DataDir         string        `yaml:"data_dir"`
	DBPath          string        `yaml:"db_path"`
	ShardDir        string        `yaml:"shard_dir"`
	LogLevel        string        `yaml:"log_level"`
	IngestInterval  time.Duration `yaml:"ingest_interval"`
	EvalInterval    time.Duration `yaml:"eval_interval"`
	LeaseInterval   time.Duration `yaml:"lease_interval"`
	LeaseTTL        time.Duration `yaml:"lease_ttl"`
	DefaultPageSize int           `yaml:"default_page_size"`
	MaxPageSize     int           `yaml:"max_page_size"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	IngestBatchSize int           `yaml:"ingest_batch_size"`
}

// Default returns a production-shaped configuration rooted at dataDir. It is a
// thin convenience over Normalize so the default table lives in exactly one
// place rather than being duplicated between the two.
func Default(dataDir string) Config {
	c := Config{DataDir: dataDir}
	_ = c.Normalize()
	return c
}

// Load reads a YAML config file (if present) and applies environment variable
// overrides. A missing file is not an error: defaults are used.
func Load(path string) (Config, error) {
	cfg := Default("")
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("read config %s: %w", path, err)
		}
		if err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return Config{}, fmt.Errorf("parse config %s: %w", path, err)
			}
		}
	}
	applyEnv(&cfg)
	if err := cfg.Normalize(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	setStr("TERMDUTY_HTTP_ADDR", &cfg.HTTPAddr)
	setStr("TERMDUTY_DATA_DIR", &cfg.DataDir)
	setStr("TERMDUTY_DB_PATH", &cfg.DBPath)
	setStr("TERMDUTY_SHARD_DIR", &cfg.ShardDir)
	setStr("TERMDUTY_LOG_LEVEL", &cfg.LogLevel)
	setDuration("TERMDUTY_INGEST_INTERVAL", &cfg.IngestInterval)
	setDuration("TERMDUTY_EVAL_INTERVAL", &cfg.EvalInterval)
	setDuration("TERMDUTY_LEASE_INTERVAL", &cfg.LeaseInterval)
	setDuration("TERMDUTY_LEASE_TTL", &cfg.LeaseTTL)
	setDuration("TERMDUTY_READ_TIMEOUT", &cfg.ReadTimeout)
	setDuration("TERMDUTY_WRITE_TIMEOUT", &cfg.WriteTimeout)
	setDuration("TERMDUTY_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout)
	setInt("TERMDUTY_DEFAULT_PAGE_SIZE", &cfg.DefaultPageSize)
	setInt("TERMDUTY_MAX_PAGE_SIZE", &cfg.MaxPageSize)
	setInt("TERMDUTY_INGEST_BATCH_SIZE", &cfg.IngestBatchSize)
}

// Normalize fills blanks, derives paths from DataDir and validates bounds.
func (c *Config) Normalize() error {
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":57615"
	}
	if c.DataDir == "" {
		c.DataDir = "./data"
	}
	if c.DBPath == "" {
		c.DBPath = c.DataDir + "/termduty.db"
	}
	if c.ShardDir == "" {
		c.ShardDir = c.DataDir + "/shards"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.IngestInterval <= 0 {
		c.IngestInterval = 5 * time.Second
	}
	if c.EvalInterval <= 0 {
		c.EvalInterval = 10 * time.Second
	}
	if c.LeaseInterval <= 0 {
		c.LeaseInterval = 15 * time.Second
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 60 * time.Second
	}
	if c.DefaultPageSize <= 0 {
		c.DefaultPageSize = 50
	}
	if c.MaxPageSize <= 0 {
		c.MaxPageSize = 200
	}
	if c.MaxPageSize < c.DefaultPageSize {
		c.MaxPageSize = c.DefaultPageSize
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 15 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 30 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	if c.IngestBatchSize <= 0 {
		c.IngestBatchSize = 50
	}
	return nil
}

func setStr(key string, dst *string) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		*dst = v
	}
}

func setDuration(key string, dst *time.Duration) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*dst = d
		} else if n, err := strconv.Atoi(v); err == nil {
			*dst = time.Duration(n) * time.Second
		}
	}
}

func setInt(key string, dst *int) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			*dst = n
		}
	}
}
