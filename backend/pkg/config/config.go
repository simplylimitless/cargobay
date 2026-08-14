package config

import (
	"os"
	"time"
)

// ServerConfig holds server configuration
type ServerConfig struct {
	Port        int           `yaml:"port"`
	Host        string        `yaml:"host"`
	ReadTimeout time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout time.Duration `yaml:"idle_timeout"`
}

// StorageConfig holds storage backend configuration
type StorageConfig struct {
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type string `yaml:"type"`
	DSN  string `yaml:"dsn"`
}

// CacheConfig holds cache configuration
type CacheConfig struct {
	Type     string        `yaml:"type"`
	URL      string        `yaml:"url"`
	TTL      time.Duration `yaml:"ttl"`
	MaxSize  int64         `yaml:"max_size"`
}

// RegistryConfig holds upstream registry configuration
type RegistryConfig struct {
	ID        string `yaml:"id"`
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	Type      string `yaml:"type"`
	Proxy     bool   `yaml:"proxy"`
	Upstream  string `yaml:"upstream"`
	Enabled   bool   `yaml:"enabled"`
	Priority  int    `yaml:"priority"`
}

// Config is the main application configuration
type Config struct {
	Server     ServerConfig      `yaml:"server"`
	Storage    StorageConfig     `yaml:"storage"`
	Database   DatabaseConfig    `yaml:"database"`
	Cache      CacheConfig       `yaml:"cache"`
	Registries []RegistryConfig  `yaml:"registries"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:        4500,
			Host:        "0.0.0.0",
			ReadTimeout: 30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout: 120 * time.Second,
		},
		Storage: StorageConfig{
			Type:   "local",
			Config: map[string]string{},
		},
		Database: DatabaseConfig{
			Type: "postgres",
			DSN:  "postgres://localhost:5432/cargobay",
		},
		Cache: CacheConfig{
			Type:  "redis",
			URL:   "redis://localhost:6379",
			TTL:   time.Hour,
			MaxSize: 1073741824, // 1GB
		},
	}
}

// LoadConfig loads configuration from environment and YAML
func LoadConfig() *Config {
	config := DefaultConfig()

	// Override with environment variables
	if port := os.Getenv("PORT"); port != "" {
		config.Server.Port = 4500 // default
	}

	if host := os.Getenv("HOST"); host != "" {
		config.Server.Host = host
	}

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		config.Cache.URL = redisURL
	}

	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		config.Database.DSN = databaseURL
	}

	return config
}
