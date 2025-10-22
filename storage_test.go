package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorage(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	storage, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create local storage: %v", err)
	}

	ctx := context.Background()

	// Test Upload
	testData := []byte("test database content")
	reader := bytes.NewReader(testData)
	if err := storage.Upload(ctx, "testdb", reader); err != nil {
		t.Fatalf("Failed to upload: %v", err)
	}

	// Test Exists
	exists, err := storage.Exists(ctx, "testdb")
	if err != nil {
		t.Fatalf("Failed to check existence: %v", err)
	}
	if !exists {
		t.Fatal("Database should exist after upload")
	}

	// Test Download
	downloadReader, err := storage.Download(ctx, "testdb")
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	if downloadReader == nil {
		t.Fatal("Download reader should not be nil")
	}
	defer downloadReader.Close()

	// Verify content
	buf := new(bytes.Buffer)
	buf.ReadFrom(downloadReader)
	if !bytes.Equal(buf.Bytes(), testData) {
		t.Fatalf("Downloaded content doesn't match. Expected %s, got %s", testData, buf.Bytes())
	}

	// Test List
	list, err := storage.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list: %v", err)
	}
	if len(list) != 1 || list[0] != "testdb" {
		t.Fatalf("Expected [testdb], got %v", list)
	}

	// Test Delete
	if err := storage.Delete(ctx, "testdb"); err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	exists, err = storage.Exists(ctx, "testdb")
	if err != nil {
		t.Fatalf("Failed to check existence after delete: %v", err)
	}
	if exists {
		t.Fatal("Database should not exist after delete")
	}
}

func TestDatabaseCache(t *testing.T) {
	tmpDir := t.TempDir()

	storage, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create local storage: %v", err)
	}

	cache := NewDatabaseCache(storage, "testdb", 5)
	defer cache.Cleanup()

	ctx := context.Background()

	// Test Download (should create empty database)
	if err := cache.Download(ctx); err != nil {
		t.Fatalf("Failed to download: %v", err)
	}

	localPath := cache.GetLocalPath()
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Fatalf("Local database file should exist at %s", localPath)
	}

	// Write some data to the local file
	testData := []byte("cached database content")
	if err := os.WriteFile(localPath, testData, 0644); err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}

	// Test Upload
	if err := cache.Upload(ctx); err != nil {
		t.Fatalf("Failed to upload: %v", err)
	}

	// Verify upload by downloading again
	downloadReader, err := storage.Download(ctx, "testdb")
	if err != nil {
		t.Fatalf("Failed to verify upload: %v", err)
	}
	defer downloadReader.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(downloadReader)
	if !bytes.Equal(buf.Bytes(), testData) {
		t.Fatalf("Uploaded content doesn't match. Expected %s, got %s", testData, buf.Bytes())
	}
}

func TestSQLiteTypeMapping(t *testing.T) {
	tests := []struct {
		sqliteType string
		pgType     string
	}{
		{"INTEGER", "int8"},
		{"INT", "int8"},
		{"TEXT", "text"},
		{"VARCHAR(255)", "text"},
		{"REAL", "float8"},
		{"FLOAT", "float8"},
		{"DOUBLE", "float8"},
		{"BLOB", "bytea"},
		{"NUMERIC", "numeric"},
		{"DATE", "date"},
		{"DATETIME", "timestamp"},
		{"BOOLEAN", "bool"},
	}

	for _, tt := range tests {
		result := SQLiteTypeToPostgres(tt.sqliteType)
		if result != tt.pgType {
			t.Errorf("SQLiteTypeToPostgres(%s) = %s; want %s", tt.sqliteType, result, tt.pgType)
		}
	}
}

func TestMapSQLiteError(t *testing.T) {
	tests := []struct {
		error    string
		sqlState string
	}{
		{"UNIQUE constraint failed", "23505"},
		{"NOT NULL constraint failed", "23502"},
		{"FOREIGN KEY constraint failed", "23503"},
		{"CHECK constraint failed", "23514"},
		{"database is locked", "40001"},
		{"no such table", "42P01"},
		{"no such column", "42703"},
		{"syntax error", "42601"},
	}

	for _, tt := range tests {
		err := &mockError{msg: tt.error}
		result := MapSQLiteError(err)
		if result != tt.sqlState {
			t.Errorf("MapSQLiteError(%s) = %s; want %s", tt.error, result, tt.sqlState)
		}
	}
}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

func TestConfigLoading(t *testing.T) {
	// Test default config
	config, err := LoadConfig("")
	if err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	if config.Server.Port != 5432 {
		t.Errorf("Expected default port 5432, got %d", config.Server.Port)
	}

	if config.Database.Name != "myapp" {
		t.Errorf("Expected default database name 'myapp', got %s", config.Database.Name)
	}

	// Test environment variable override
	os.Setenv("PG_PORT", "5433")
	os.Setenv("DB_NAME", "testdb")
	defer os.Unsetenv("PG_PORT")
	defer os.Unsetenv("DB_NAME")

	config, err = LoadConfig("")
	if err != nil {
		t.Fatalf("Failed to load config with env vars: %v", err)
	}

	if config.Server.Port != 5433 {
		t.Errorf("Expected port 5433 from env var, got %d", config.Server.Port)
	}

	if config.Database.Name != "testdb" {
		t.Errorf("Expected database name 'testdb' from env var, got %s", config.Database.Name)
	}
}

func TestConfigFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
server:
  port: 5434
  host: 127.0.0.1
  authentication:
    user: testuser
    password: testpass

database:
  name: yamldb
  sqlite_path: /tmp/yamldb.sqlite
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config from file: %v", err)
	}

	if config.Server.Port != 5434 {
		t.Errorf("Expected port 5434 from YAML, got %d", config.Server.Port)
	}

	if config.Server.Host != "127.0.0.1" {
		t.Errorf("Expected host 127.0.0.1 from YAML, got %s", config.Server.Host)
	}

	if config.Database.Name != "yamldb" {
		t.Errorf("Expected database name 'yamldb' from YAML, got %s", config.Database.Name)
	}
}
