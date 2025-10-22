package main

import (
	"os"
	"testing"
)

func TestSQLiteBackend(t *testing.T) {
	// Create temporary database
	tmpFile, err := os.CreateTemp("", "test-*.sqlite")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Create backend
	backend, err := NewSQLiteBackend(tmpFile.Name(), "deferred", 5)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	connID := "test-conn-1"

	// Test simple query
	_, err = backend.Exec(connID, "CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Test insert
	result, err := backend.Exec(connID, "INSERT INTO test (id, name) VALUES (1, 'Alice')")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("Expected 1 row affected, got %d", rowsAffected)
	}

	// Test select
	rows, err := backend.Query(connID, "SELECT id, name FROM test WHERE id = 1")
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("Expected at least one row")
	}

	var id int
	var name string
	if err := rows.Scan(&id, &name); err != nil {
		t.Fatalf("Failed to scan row: %v", err)
	}

	if id != 1 || name != "Alice" {
		t.Errorf("Expected id=1, name=Alice, got id=%d, name=%s", id, name)
	}
}

func TestTransactions(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.sqlite")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	backend, err := NewSQLiteBackend(tmpFile.Name(), "deferred", 5)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	connID := "test-conn-2"

	// Create table
	_, err = backend.Exec(connID, "CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Test transaction commit
	if err := backend.BeginTransaction(connID, "deferred"); err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	_, err = backend.Exec(connID, "INSERT INTO test (id, value) VALUES (1, 'test1')")
	if err != nil {
		t.Fatalf("Failed to insert in transaction: %v", err)
	}

	if err := backend.CommitTransaction(connID); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}

	// Verify commit
	rows, err := backend.Query(connID, "SELECT COUNT(*) FROM test")
	if err != nil {
		t.Fatalf("Failed to query after commit: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("Expected row count")
	}

	var count int
	if err := rows.Scan(&count); err != nil {
		t.Fatalf("Failed to scan count: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected count=1 after commit, got %d", count)
	}

	// Test transaction rollback
	connID2 := "test-conn-3"
	if err := backend.BeginTransaction(connID2, "deferred"); err != nil {
		t.Fatalf("Failed to begin second transaction: %v", err)
	}

	_, err = backend.Exec(connID2, "INSERT INTO test (id, value) VALUES (2, 'test2')")
	if err != nil {
		t.Fatalf("Failed to insert in second transaction: %v", err)
	}

	if err := backend.RollbackTransaction(connID2); err != nil {
		t.Fatalf("Failed to rollback transaction: %v", err)
	}

	// Verify rollback
	rows2, err := backend.Query(connID2, "SELECT COUNT(*) FROM test")
	if err != nil {
		t.Fatalf("Failed to query after rollback: %v", err)
	}
	defer rows2.Close()

	if !rows2.Next() {
		t.Fatal("Expected row count after rollback")
	}

	if err := rows2.Scan(&count); err != nil {
		t.Fatalf("Failed to scan count after rollback: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected count=1 after rollback, got %d", count)
	}
}

func TestPreparedStatements(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.sqlite")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	backend, err := NewSQLiteBackend(tmpFile.Name(), "deferred", 5)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	connID := "test-conn-4"

	// Create table
	_, err = backend.Exec(connID, "CREATE TABLE test (id INTEGER, name TEXT)")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Prepare statement
	if err := backend.Prepare(connID, "insert_test", "INSERT INTO test (id, name) VALUES (?, ?)"); err != nil {
		t.Fatalf("Failed to prepare statement: %v", err)
	}

	// Execute prepared statement (note: ExecutePrepared returns rows, not result)
	// For INSERT, we'd normally use Exec, but let's test the prepare/execute flow
	conn := backend.GetOrCreateConnection(connID)
	stmt := conn.PreparedStmts["insert_test"]
	if stmt == nil {
		t.Fatal("Prepared statement not found")
	}

	_, err = stmt.Exec(1, "Alice")
	if err != nil {
		t.Fatalf("Failed to execute prepared statement: %v", err)
	}

	// Verify
	rows, err := backend.Query(connID, "SELECT id, name FROM test WHERE id = 1")
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("Expected at least one row")
	}

	var id int
	var name string
	if err := rows.Scan(&id, &name); err != nil {
		t.Fatalf("Failed to scan: %v", err)
	}

	if id != 1 || name != "Alice" {
		t.Errorf("Expected id=1, name=Alice, got id=%d, name=%s", id, name)
	}

	// Close prepared statement
	if err := backend.ClosePrepared(connID, "insert_test"); err != nil {
		t.Fatalf("Failed to close prepared statement: %v", err)
	}
}

func TestConnectionCleanup(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.sqlite")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	backend, err := NewSQLiteBackend(tmpFile.Name(), "deferred", 5)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	connID := "test-conn-5"

	// Create table
	_, err = backend.Exec(connID, "CREATE TABLE test (id INTEGER)")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Start transaction
	if err := backend.BeginTransaction(connID, "deferred"); err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Prepare statement
	if err := backend.Prepare(connID, "test_stmt", "INSERT INTO test (id) VALUES (?)"); err != nil {
		t.Fatalf("Failed to prepare statement: %v", err)
	}

	// Remove connection (should clean up transaction and statements)
	backend.RemoveConnection(connID)

	// Verify connection is removed
	backend.mu.Lock()
	_, exists := backend.connections[connID]
	backend.mu.Unlock()

	if exists {
		t.Error("Connection should be removed")
	}
}
