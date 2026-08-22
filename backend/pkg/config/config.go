package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration so it can be unmarshaled from YAML either as
// a plain integer (seconds) or a duration string like "30s"/"1h".
type Duration time.Duration

// Std converts to the standard library's time.Duration.
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	switch value.Tag {
	case "!!int":
		var seconds int64
		if err := value.Decode(&seconds); err != nil {
			return err
		}
		*d = Duration(time.Duration(seconds) * time.Second)
		return nil
	case "!!str":
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	default:
		return fmt.Errorf("unsupported duration value tag: %s", value.Tag)
	}
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port         int      `yaml:"port"`
	Host         string   `yaml:"host"`
	ReadTimeout  Duration `yaml:"read_timeout"`
	WriteTimeout Duration `yaml:"write_timeout"`
	IdleTimeout  Duration `yaml:"idle_timeout"`
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
	Type    string   `yaml:"type"`
	URL     string   `yaml:"url"`
	TTL     Duration `yaml:"ttl"`
	MaxSize int64    `yaml:"max_size"`
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
	Server     ServerConfig     `yaml:"server"`
	Storage    StorageConfig    `yaml:"storage"`
	Database   DatabaseConfig   `yaml:"database"`
	Cache      CacheConfig      `yaml:"cache"`
	Registries []RegistryConfig `yaml:"registries"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:         4500,
			Host:         "0.0.0.0",
			ReadTimeout:  Duration(30 * time.Second),
			WriteTimeout: Duration(30 * time.Second),
			IdleTimeout:  Duration(120 * time.Second),
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
			Type:    "redis",
			URL:     "redis://localhost:6379",
			TTL:     Duration(time.Hour),
			MaxSize: 1073741824, // 1GB
		},
	}
}

// LoadConfig loads configuration from a YAML file, then applies environment
// variable overrides on top.
func LoadConfig() *Config {
	config := DefaultConfig()

	configFile := os.Getenv("CONFIG_FILE")
	if configFile == "" {
		configFile = "./config.yaml"
	}

	if data, err := os.ReadFile(configFile); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("warning: failed to read config file %s: %v", configFile, err)
		}
	} else if err := yaml.Unmarshal(data, config); err != nil {
		log.Printf("warning: failed to parse config file %s: %v", configFile, err)
	}

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

	if storageType := os.Getenv("STORAGE_TYPE"); storageType != "" {
		config.Storage.Type = storageType
	}

	if config.Storage.Config == nil {
		config.Storage.Config = map[string]string{}
	}

	if accessKey := os.Getenv("S3_ACCESS_KEY"); accessKey != "" {
		config.Storage.Config["access_key"] = accessKey
	}

	if secretKey := os.Getenv("S3_SECRET_KEY"); secretKey != "" {
		config.Storage.Config["secret_key"] = secretKey
	}

	return config
}
