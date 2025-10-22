package main

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config represents the server configuration
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Storage  StorageConfig  `yaml:"storage"`
	Logging  LoggingConfig  `yaml:"logging"`
}

// ServerConfig contains PostgreSQL server settings
type ServerConfig struct {
	Port           int                `yaml:"port"`
	Host           string             `yaml:"host"`
	Authentication AuthenticationConfig `yaml:"authentication"`
}

// AuthenticationConfig contains authentication settings
type AuthenticationConfig struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// DatabaseConfig contains SQLite database settings
type DatabaseConfig struct {
	Name            string `yaml:"name"`
	SQLitePath      string `yaml:"sqlite_path"`
	TransactionMode string `yaml:"transaction_mode"`
	ConnectionPoolSize int `yaml:"connection_pool_size"`
}

// StorageConfig contains blob storage settings
type StorageConfig struct {
	Backend string       `yaml:"backend"`
	Local   LocalConfig  `yaml:"local"`
	S3      S3Config     `yaml:"s3"`
	Azure   AzureConfig  `yaml:"azure"`
	CacheTTLMinutes int  `yaml:"cache_ttl_minutes"`
}

// LocalConfig contains local filesystem storage settings
type LocalConfig struct {
	BasePath string `yaml:"base_path"`
}

// S3Config contains AWS S3 storage settings
type S3Config struct {
	Bucket string `yaml:"bucket"`
	Region string `yaml:"region"`
	Prefix string `yaml:"prefix"`
}

// AzureConfig contains Azure Blob Storage settings
type AzureConfig struct {
	Account            string `yaml:"account"`
	Container          string `yaml:"container"`
	Key                string `yaml:"key"`
	UseManagedIdentity bool   `yaml:"use_managed_identity"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// LoadConfig loads configuration from file and environment variables
func LoadConfig(configPath string) (*Config, error) {
	config := &Config{
		Server: ServerConfig{
			Port: 5432,
			Host: "0.0.0.0",
			Authentication: AuthenticationConfig{
				User:     "postgres",
				Password: "postgres",
			},
		},
		Database: DatabaseConfig{
			Name:               "myapp",
			SQLitePath:         "/tmp/myapp.sqlite",
			TransactionMode:    "deferred",
			ConnectionPoolSize: 10,
		},
		Storage: StorageConfig{
			Backend: "local",
			Local: LocalConfig{
				BasePath: "./data",
			},
			CacheTTLMinutes: 5,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}

	// Load from config file if it exists
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to read config file: %w", err)
			}
		} else {
			if err := yaml.Unmarshal(data, config); err != nil {
				return nil, fmt.Errorf("failed to parse config file: %w", err)
			}
		}
	}

	// Override with environment variables
	if val := os.Getenv("PG_PORT"); val != "" {
		port, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("invalid PG_PORT: %w", err)
		}
		config.Server.Port = port
	}
	if val := os.Getenv("PG_HOST"); val != "" {
		config.Server.Host = val
	}
	if val := os.Getenv("PG_USER"); val != "" {
		config.Server.Authentication.User = val
	}
	if val := os.Getenv("PG_PASSWORD"); val != "" {
		config.Server.Authentication.Password = val
	}
	if val := os.Getenv("DB_NAME"); val != "" {
		config.Database.Name = val
	}
	if val := os.Getenv("DB_PATH"); val != "" {
		config.Database.SQLitePath = val
	}
	if val := os.Getenv("STORAGE"); val != "" {
		config.Storage.Backend = val
	}
	if val := os.Getenv("S3_BUCKET"); val != "" {
		config.Storage.S3.Bucket = val
	}
	if val := os.Getenv("S3_REGION"); val != "" {
		config.Storage.S3.Region = val
	}
	if val := os.Getenv("S3_PREFIX"); val != "" {
		config.Storage.S3.Prefix = val
	}
	if val := os.Getenv("AZURE_STORAGE_ACCOUNT"); val != "" {
		config.Storage.Azure.Account = val
	}
	if val := os.Getenv("AZURE_STORAGE_CONTAINER"); val != "" {
		config.Storage.Azure.Container = val
	}
	if val := os.Getenv("AZURE_STORAGE_KEY"); val != "" {
		config.Storage.Azure.Key = val
	}
	if val := os.Getenv("LOG_LEVEL"); val != "" {
		config.Logging.Level = val
	}
	if val := os.Getenv("LOG_FORMAT"); val != "" {
		config.Logging.Format = val
	}
	if val := os.Getenv("CONNECTION_POOL_SIZE"); val != "" {
		poolSize, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("invalid CONNECTION_POOL_SIZE: %w", err)
		}
		config.Database.ConnectionPoolSize = poolSize
	}
	if val := os.Getenv("CACHE_TTL_MINUTES"); val != "" {
		ttl, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("invalid CACHE_TTL_MINUTES: %w", err)
		}
		config.Storage.CacheTTLMinutes = ttl
	}

	return config, nil
}
