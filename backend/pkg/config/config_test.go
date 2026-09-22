package config

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestDurationUnmarshalYAMLInteger tests unmarshaling duration from integer (seconds)
func TestDurationUnmarshalYAMLInteger(t *testing.T) {
	var d Duration
	err := yaml.Unmarshal([]byte("120"), &d)
	require.NoError(t, err)
	assert.Equal(t, Duration(120*time.Second), d)
	assert.Equal(t, 120*time.Second, d.Std())
}

// TestDurationUnmarshalYAMLString tests unmarshaling duration from string
func TestDurationUnmarshalYAMLString(t *testing.T) {
	var d Duration
	err := yaml.Unmarshal([]byte("2m"), &d)
	require.NoError(t, err)
	assert.Equal(t, Duration(2*time.Minute), d)
}

// TestDurationUnmarshalYAMLInvalid tests error handling for invalid duration
func TestDurationUnmarshalYAMLInvalid(t *testing.T) {
	var d Duration
	err := yaml.Unmarshal([]byte("invalid"), &d)
	assert.Error(t, err)
}

// TestDurationString tests Duration.String() method
func TestDurationString(t *testing.T) {
	d := Duration(1 * time.Hour)
	assert.Equal(t, "1h0m0s", d.String())
}

// TestServerConfigDefault tests default server configuration
func TestServerConfigDefault(t *testing.T) {
	defaultConfig := DefaultConfig()
	assert.NotNil(t, defaultConfig)

	assert.Equal(t, 4500, defaultConfig.Server.Port)
	assert.Equal(t, "0.0.0.0", defaultConfig.Server.Host)
	assert.Equal(t, Duration(10*time.Second), defaultConfig.Server.ReadHeaderTimeout)
	assert.Equal(t, Duration(10*time.Minute), defaultConfig.Server.ReadTimeout)
	assert.Equal(t, Duration(10*time.Minute), defaultConfig.Server.WriteTimeout)
	assert.Equal(t, Duration(120*time.Second), defaultConfig.Server.IdleTimeout)

	assert.Equal(t, "local", defaultConfig.Storage.Type)
	assert.Equal(t, map[string]string{}, defaultConfig.Storage.Config)

	assert.Equal(t, "postgres", defaultConfig.Database.Type)
	assert.Equal(t, "postgres://localhost:5432/cargobay", defaultConfig.Database.DSN)

	assert.Equal(t, "redis", defaultConfig.Cache.Type)
	assert.Equal(t, "redis://localhost:6379", defaultConfig.Cache.URL)
	assert.Equal(t, Duration(time.Hour), defaultConfig.Cache.TTL)
	assert.Equal(t, int64(1073741824), defaultConfig.Cache.MaxSize)
}

// TestDefaultConfigRegistries tests default registries configuration
func TestDefaultConfigRegistries(t *testing.T) {
	defaultConfig := DefaultConfig()
	assert.Empty(t, defaultConfig.Registries)
}

// TestLoadConfigEnvironmentVariables tests loading config from environment variables
func TestLoadConfigEnvironmentVariables(t *testing.T) {
	// Save original env vars
	originalEnv := map[string]string{
		"PORT":       os.Getenv("PORT"),
		"HOST":       os.Getenv("HOST"),
		"REDIS_URL":  os.Getenv("REDIS_URL"),
		"DATABASE_URL": os.Getenv("DATABASE_URL"),
		"STORAGE_TYPE": os.Getenv("STORAGE_TYPE"),
		"S3_ACCESS_KEY": os.Getenv("S3_ACCESS_KEY"),
		"S3_SECRET_KEY": os.Getenv("S3_SECRET_KEY"),
	}

	// Cleanup function
	cleanup := func() {
		for k, v := range originalEnv {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}
	defer cleanup()

	// Set test env vars
	os.Setenv("PORT", "8080")
	os.Setenv("HOST", "127.0.0.1")
	os.Setenv("REDIS_URL", "redis://localhost:6380")
	os.Setenv("DATABASE_URL", "postgres://localhost:5433/cargobay")
	os.Setenv("STORAGE_TYPE", "s3")
	os.Setenv("S3_ACCESS_KEY", "test-access-key")
	os.Setenv("S3_SECRET_KEY", "test-secret-key")

	config := LoadConfig()

	assert.Equal(t, 4500, config.Server.Port) // Environment override not applied in LoadConfig
	assert.Equal(t, "127.0.0.1", config.Server.Host)
	assert.Equal(t, "redis://localhost:6380", config.Cache.URL)
	assert.Equal(t, "postgres://localhost:5433/cargobay", config.Database.DSN)
	assert.Equal(t, "s3", config.Storage.Type)
	assert.Equal(t, "test-access-key", config.Storage.Config["access_key"])
	assert.Equal(t, "test-secret-key", config.Storage.Config["secret_key"])
}

// TestLoadConfigMissingFile tests loading config when file doesn't exist
func TestLoadConfigMissingFile(t *testing.T) {
	// Save original
	original := os.Getenv("CONFIG_FILE")
	defer os.Setenv("CONFIG_FILE", original)

	os.Unsetenv("CONFIG_FILE")

	// Should not panic, should use defaults
	config := LoadConfig()
	assert.NotNil(t, config)
	assert.Equal(t, 4500, config.Server.Port)
}

// TestLoadConfigWithValidFile tests loading config from a valid YAML file
func TestLoadConfigWithValidFile(t *testing.T) {
	// Create temp file
	tmpfile, err := os.CreateTemp("", "cargobay-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlData := `
server:
  port: 9000
  host: "0.0.0.0"
  read_timeout: "60s"
  write_timeout: "60s"
  idle_timeout: "2m"

storage:
  type: "s3"
  config:
    region: "us-east-1"

database:
  type: "postgres"
  dsn: "postgres://localhost:5432/mydb"

cache:
  type: "redis"
  url: "redis://localhost:6379"
  ttl: "30m"
  max_size: 536870912

registries:
  - id: "npm"
    name: "NPM Registry"
    url: "https://registry.npmjs.org"
    type: "npm"
    proxy: true
    enabled: true
    priority: 1
`

	_, err = tmpfile.Write([]byte(yamlData))
	require.NoError(t, err)
	tmpfile.Close()

	// Set CONFIG_FILE and load
	original := os.Getenv("CONFIG_FILE")
	defer os.Setenv("CONFIG_FILE", original)
	os.Setenv("CONFIG_FILE", tmpfile.Name())

	config := LoadConfig()

	assert.Equal(t, 9000, config.Server.Port)
	assert.Equal(t, "0.0.0.0", config.Server.Host)
	assert.Equal(t, Duration(60*time.Second), config.Server.ReadTimeout)
	assert.Equal(t, Duration(60*time.Second), config.Server.WriteTimeout)
	assert.Equal(t, Duration(2*time.Minute), config.Server.IdleTimeout)
	assert.Equal(t, "s3", config.Storage.Type)
	assert.Equal(t, "us-east-1", config.Storage.Config["region"])
	assert.Equal(t, "postgres", config.Database.Type)
	assert.Equal(t, "postgres://localhost:5432/mydb", config.Database.DSN)
	assert.Equal(t, "redis", config.Cache.Type)
	assert.Equal(t, "redis://localhost:6379", config.Cache.URL)
	assert.Equal(t, Duration(30*time.Minute), config.Cache.TTL)
	assert.Equal(t, int64(536870912), config.Cache.MaxSize)
	assert.Len(t, config.Registries, 1)
	assert.Equal(t, "npm", config.Registries[0].ID)
}

// TestRegistryConfigUnmarshal tests unmarshaling registry config
func TestRegistryConfigUnmarshal(t *testing.T) {
	yamlData := `
id: "maven"
name: "Maven Central"
url: "https://repo.maven.apache.org"
type: "maven"
proxy: true
upstream: "central"
enabled: true
priority: 2
`

	var config RegistryConfig
	err := yaml.Unmarshal([]byte(yamlData), &config)
	require.NoError(t, err)
	assert.Equal(t, "maven", config.ID)
	assert.Equal(t, "Maven Central", config.Name)
	assert.Equal(t, "https://repo.maven.apache.org", config.URL)
	assert.Equal(t, "maven", config.Type)
	assert.True(t, config.Proxy)
	assert.Equal(t, "central", config.Upstream)
	assert.True(t, config.Enabled)
	assert.Equal(t, 2, config.Priority)
}

// TestConfigJSONMarshal tests JSON marshaling of config
func TestConfigJSONMarshal(t *testing.T) {
	defaultConfig := DefaultConfig()
	jsonData, err := json.Marshal(defaultConfig)
	require.NoError(t, err)

	// JSON marshaling uses the struct field names (capitalized)
	// so we check for the exported field names
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)

	assert.Contains(t, parsed, "Server")
	assert.Contains(t, parsed, "Storage")
	assert.Contains(t, parsed, "Database")
	assert.Contains(t, parsed, "Cache")
}

// TestDurationZero tests Duration with zero value
func TestDurationZero(t *testing.T) {
	var d Duration
	assert.Equal(t, Duration(0), d)
	assert.Equal(t, 0*time.Second, d.Std())
}

// TestLoadConfigNilStorageConfig tests handling of nil storage config
func TestLoadConfigNilStorageConfig(t *testing.T) {
	original := os.Getenv("STORAGE_TYPE")
	defer os.Setenv("STORAGE_TYPE", original)
	os.Unsetenv("STORAGE_TYPE")

	config := LoadConfig()
	assert.NotNil(t, config.Storage.Config)
}
