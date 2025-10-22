package main

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteBackend manages SQLite database connections and operations
type SQLiteBackend struct {
	db              *sql.DB
	dbPath          string
	transactionMode string
	mu              sync.RWMutex
	connections     map[string]*ConnectionState
}

// ConnectionState tracks the state of a client connection
type ConnectionState struct {
	ID           string
	Tx           *sql.Tx
	InTx         bool
	TxStatus     TransactionStatus
	PreparedStmts map[string]*sql.Stmt
	mu           sync.Mutex
}

// TransactionStatus represents the current transaction status
type TransactionStatus int

const (
	TxIdle TransactionStatus = iota
	TxInTransaction
	TxFailed
)

// NewSQLiteBackend creates a new SQLite backend
func NewSQLiteBackend(dbPath string, transactionMode string, maxConnections int) (*SQLiteBackend, error) {
	// Open SQLite database with connection pooling
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_timeout=5000&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections / 2)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	return &SQLiteBackend{
		db:              db,
		dbPath:          dbPath,
		transactionMode: transactionMode,
		connections:     make(map[string]*ConnectionState),
	}, nil
}

// GetOrCreateConnection gets or creates a connection state for a connection ID
func (b *SQLiteBackend) GetOrCreateConnection(connectionID string) *ConnectionState {
	b.mu.Lock()
	defer b.mu.Unlock()

	conn, exists := b.connections[connectionID]
	if !exists {
		conn = &ConnectionState{
			ID:            connectionID,
			PreparedStmts: make(map[string]*sql.Stmt),
			TxStatus:      TxIdle,
		}
		b.connections[connectionID] = conn
	}
	return conn
}

// RemoveConnection removes a connection state
func (b *SQLiteBackend) RemoveConnection(connectionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if conn, exists := b.connections[connectionID]; exists {
		// Clean up prepared statements
		conn.mu.Lock()
		for _, stmt := range conn.PreparedStmts {
			stmt.Close()
		}
		conn.mu.Unlock()

		// Rollback any active transaction
		if conn.InTx && conn.Tx != nil {
			conn.Tx.Rollback()
		}

		delete(b.connections, connectionID)
	}
}

// BeginTransaction starts a new transaction
func (b *SQLiteBackend) BeginTransaction(connectionID string, mode string) error {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.InTx {
		return fmt.Errorf("transaction already in progress")
	}

	// Determine transaction mode
	txMode := mode
	if txMode == "" {
		txMode = b.transactionMode
	}

	// Begin transaction with appropriate mode
	var tx *sql.Tx
	var err error

	switch strings.ToLower(txMode) {
	case "immediate":
		tx, err = b.db.Begin()
		if err == nil {
			_, err = tx.Exec("BEGIN IMMEDIATE")
		}
	case "exclusive":
		tx, err = b.db.Begin()
		if err == nil {
			_, err = tx.Exec("BEGIN EXCLUSIVE")
		}
	default: // deferred
		tx, err = b.db.Begin()
	}

	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	conn.Tx = tx
	conn.InTx = true
	conn.TxStatus = TxInTransaction

	return nil
}

// CommitTransaction commits the current transaction
func (b *SQLiteBackend) CommitTransaction(connectionID string) error {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if !conn.InTx || conn.Tx == nil {
		return fmt.Errorf("no transaction in progress")
	}

	if err := conn.Tx.Commit(); err != nil {
		conn.TxStatus = TxFailed
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	conn.Tx = nil
	conn.InTx = false
	conn.TxStatus = TxIdle

	return nil
}

// RollbackTransaction rolls back the current transaction
func (b *SQLiteBackend) RollbackTransaction(connectionID string) error {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if !conn.InTx || conn.Tx == nil {
		return fmt.Errorf("no transaction in progress")
	}

	if err := conn.Tx.Rollback(); err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}

	conn.Tx = nil
	conn.InTx = false
	conn.TxStatus = TxIdle

	return nil
}

// Query executes a query and returns rows
func (b *SQLiteBackend) Query(connectionID string, query string, args ...interface{}) (*sql.Rows, error) {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	// Use transaction if active, otherwise use regular connection
	if conn.InTx && conn.Tx != nil {
		return conn.Tx.Query(query, args...)
	}

	return b.db.Query(query, args...)
}

// Exec executes a query that doesn't return rows
func (b *SQLiteBackend) Exec(connectionID string, query string, args ...interface{}) (sql.Result, error) {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	// Use transaction if active, otherwise use regular connection
	if conn.InTx && conn.Tx != nil {
		return conn.Tx.Exec(query, args...)
	}

	return b.db.Exec(query, args...)
}

// Prepare prepares a statement
func (b *SQLiteBackend) Prepare(connectionID string, name string, query string) error {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	// Close existing statement with same name
	if stmt, exists := conn.PreparedStmts[name]; exists {
		stmt.Close()
	}

	// Prepare new statement
	var stmt *sql.Stmt
	var err error

	if conn.InTx && conn.Tx != nil {
		stmt, err = conn.Tx.Prepare(query)
	} else {
		stmt, err = b.db.Prepare(query)
	}

	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}

	conn.PreparedStmts[name] = stmt
	return nil
}

// ExecutePrepared executes a prepared statement
func (b *SQLiteBackend) ExecutePrepared(connectionID string, name string, args ...interface{}) (*sql.Rows, error) {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	stmt, exists := conn.PreparedStmts[name]
	if !exists {
		return nil, fmt.Errorf("prepared statement not found: %s", name)
	}

	return stmt.Query(args...)
}

// ClosePrepared closes a prepared statement
func (b *SQLiteBackend) ClosePrepared(connectionID string, name string) error {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	stmt, exists := conn.PreparedStmts[name]
	if !exists {
		return nil // Already closed or doesn't exist
	}

	if err := stmt.Close(); err != nil {
		return fmt.Errorf("failed to close prepared statement: %w", err)
	}

	delete(conn.PreparedStmts, name)
	return nil
}

// GetTransactionStatus returns the current transaction status
func (b *SQLiteBackend) GetTransactionStatus(connectionID string) TransactionStatus {
	conn := b.GetOrCreateConnection(connectionID)
	conn.mu.Lock()
	defer conn.mu.Unlock()

	return conn.TxStatus
}

// Close closes the SQLite database
func (b *SQLiteBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Clean up all connections
	for id := range b.connections {
		conn := b.connections[id]
		conn.mu.Lock()

		// Close prepared statements
		for _, stmt := range conn.PreparedStmts {
			stmt.Close()
		}

		// Rollback any active transactions
		if conn.InTx && conn.Tx != nil {
			conn.Tx.Rollback()
		}

		conn.mu.Unlock()
	}

	// Close database
	if err := b.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	return nil
}

// SQLiteTypeToPostgres maps SQLite types to PostgreSQL types
func SQLiteTypeToPostgres(sqliteType string) string {
	sqliteType = strings.ToUpper(strings.TrimSpace(sqliteType))

	// Handle common SQLite type affinities
	switch {
	case strings.Contains(sqliteType, "INT"):
		return "int8"
	case strings.Contains(sqliteType, "CHAR") || strings.Contains(sqliteType, "CLOB") || strings.Contains(sqliteType, "TEXT"):
		return "text"
	case strings.Contains(sqliteType, "BLOB"):
		return "bytea"
	case strings.Contains(sqliteType, "REAL") || strings.Contains(sqliteType, "FLOA") || strings.Contains(sqliteType, "DOUB"):
		return "float8"
	case strings.Contains(sqliteType, "NUMERIC") || strings.Contains(sqliteType, "DECIMAL"):
		return "numeric"
	case strings.Contains(sqliteType, "DATE"):
		return "date"
	case strings.Contains(sqliteType, "TIME"):
		return "timestamp"
	case strings.Contains(sqliteType, "BOOL"):
		return "bool"
	default:
		return "text" // Default to text for unknown types
	}
}

// MapSQLiteError maps SQLite error codes to PostgreSQL SQLSTATE codes
func MapSQLiteError(err error) string {
	if err == nil {
		return "00000" // Success
	}

	errMsg := err.Error()

	// Map common SQLite errors to PostgreSQL SQLSTATE codes
	switch {
	case strings.Contains(errMsg, "UNIQUE constraint failed"):
		return "23505" // unique_violation
	case strings.Contains(errMsg, "NOT NULL constraint failed"):
		return "23502" // not_null_violation
	case strings.Contains(errMsg, "FOREIGN KEY constraint failed"):
		return "23503" // foreign_key_violation
	case strings.Contains(errMsg, "CHECK constraint failed"):
		return "23514" // check_violation
	case strings.Contains(errMsg, "constraint failed"):
		return "23000" // integrity_constraint_violation
	case strings.Contains(errMsg, "database is locked"):
		return "40001" // serialization_failure
	case strings.Contains(errMsg, "disk I/O error"):
		return "08006" // connection_failure
	case strings.Contains(errMsg, "database disk image is malformed"):
		return "58030" // io_error
	case strings.Contains(errMsg, "no such table"):
		return "42P01" // undefined_table
	case strings.Contains(errMsg, "no such column"):
		return "42703" // undefined_column
	case strings.Contains(errMsg, "syntax error"):
		return "42601" // syntax_error
	case strings.Contains(errMsg, "out of memory"):
		return "53200" // out_of_memory
	case strings.Contains(errMsg, "disk full"):
		return "53100" // disk_full
	default:
		return "XX000" // internal_error
	}
}
