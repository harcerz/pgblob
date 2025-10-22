package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	wire "github.com/jeroenrinzema/psql-wire"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("FATAL: Server error: %v", err)
	}
}

func run() error {
	// Load configuration
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logging
	setupLogging(config.Logging)

	log.Printf("INFO: Starting PostgreSQL Wire Protocol Server")
	log.Printf("INFO: Storage backend: %s", config.Storage.Backend)
	log.Printf("INFO: Database: %s", config.Database.Name)

	// Create blob storage backend
	storage, err := NewBlobStorage(config)
	if err != nil {
		return fmt.Errorf("failed to create storage backend: %w", err)
	}

	// Create database cache
	cache := NewDatabaseCache(storage, config.Database.Name, config.Storage.CacheTTLMinutes)
	defer func() {
		if err := cache.Cleanup(); err != nil {
			log.Printf("WARN: Failed to cleanup cache: %v", err)
		}
	}()

	// Download database from blob storage
	log.Printf("INFO: Downloading database from blob storage...")
	ctx := context.Background()
	if err := cache.Download(ctx); err != nil {
		return fmt.Errorf("failed to download database: %w", err)
	}
	log.Printf("INFO: Database downloaded to: %s", cache.GetLocalPath())

	// Create SQLite backend
	backend, err := NewSQLiteBackend(
		cache.GetLocalPath(),
		config.Database.TransactionMode,
		config.Database.ConnectionPoolSize,
	)
	if err != nil {
		return fmt.Errorf("failed to create SQLite backend: %w", err)
	}
	defer backend.Close()

	// Create transaction manager
	txManager := NewTransactionManager(backend, cache)
	defer txManager.Stop()

	// Create transaction monitor
	txMonitor := NewTransactionMonitor()

	// Create wire handler
	handler := NewSimpleWireHandler(backend, txManager, txMonitor, config)

	// Setup server parameters
	params := wire.Parameters{
		wire.ParamServerVersion:  "13.0",
		wire.ParamServerEncoding: "UTF8",
		wire.ParamClientEncoding: "UTF8",
		wire.ParameterStatus("DateStyle"):  "ISO, MDY",
		wire.ParameterStatus("TimeZone"):   "UTC",
	}

	// Create authentication strategy
	authStrategy := wire.ClearTextPassword(func(username, password string) (bool, error) {
		if username != config.Server.Authentication.User || password != config.Server.Authentication.Password {
			return false, fmt.Errorf("authentication failed for user: %s", username)
		}
		log.Printf("INFO: User authenticated: %s", username)
		return true, nil
	})

	// Create PostgreSQL wire server
	server, err := wire.NewServer(
		handler.ParseQuery,
		wire.GlobalParameters(params),
		wire.SessionAuthStrategy(authStrategy),
	)
	if err != nil {
		return fmt.Errorf("failed to create wire server: %w", err)
	}

	// Start server in a goroutine
	addr := fmt.Sprintf("%s:%d", config.Server.Host, config.Server.Port)
	log.Printf("INFO: Starting server on %s", addr)

	errChan := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(addr); err != nil {
			errChan <- fmt.Errorf("server listen error: %w", err)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return err
	case sig := <-sigChan:
		log.Printf("INFO: Received signal: %v", sig)
		log.Printf("INFO: Shutting down gracefully...")

		// Close server
		if err := server.Close(); err != nil {
			log.Printf("WARN: Error closing server: %v", err)
		}

		// Final upload to blob storage
		log.Printf("INFO: Uploading final database state to blob storage...")
		uploadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := txManager.ForceUpload(uploadCtx); err != nil {
			log.Printf("ERROR: Failed to upload database on shutdown: %v", err)
		} else {
			log.Printf("INFO: Database uploaded successfully")
		}

		// Print metrics
		metrics := txMonitor.GetMetrics()
		log.Printf("INFO: Transaction metrics:")
		log.Printf("  - Total transactions: %d", metrics.TotalTransactions)
		log.Printf("  - Committed: %d", metrics.CommittedTx)
		log.Printf("  - Rolled back: %d", metrics.RolledBackTx)
		log.Printf("  - Average duration: %v", metrics.AvgTxDuration)
		log.Printf("  - Longest transaction: %v", metrics.LongestTx)

		log.Printf("INFO: Server stopped")
		return nil
	}
}

func setupLogging(config LoggingConfig) {
	// Setup log level and format
	logFlags := log.Ldate | log.Ltime

	if config.Format == "json" {
		// For JSON logging, you might want to use a structured logging library
		// For now, we'll just add more context
		logFlags = log.Ldate | log.Ltime | log.Lmicroseconds
	}

	if config.Level == "debug" {
		logFlags |= log.Lshortfile
	}

	log.SetFlags(logFlags)
	log.SetOutput(os.Stdout)

	// Filter log level
	switch config.Level {
	case "debug":
		log.SetPrefix("DEBUG: ")
	case "info":
		log.SetPrefix("INFO: ")
	case "warn":
		log.SetPrefix("WARN: ")
	case "error":
		log.SetPrefix("ERROR: ")
	}
}
