package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	wire "github.com/jeroenrinzema/psql-wire"
	"github.com/lib/pq/oid"
)

// SimpleWireHandler provides a simplified wire protocol handler
type SimpleWireHandler struct {
	backend   *SQLiteBackend
	txManager *TransactionManager
	txMonitor *TransactionMonitor
	config    *Config
}

// NewSimpleWireHandler creates a new simplified wire protocol handler
func NewSimpleWireHandler(backend *SQLiteBackend, txManager *TransactionManager, txMonitor *TransactionMonitor, config *Config) *SimpleWireHandler {
	return &SimpleWireHandler{
		backend:   backend,
		txManager: txManager,
		txMonitor: txMonitor,
		config:    config,
	}
}

// ParseQuery implements the psql-wire ParseFn interface
func (h *SimpleWireHandler) ParseQuery(ctx context.Context, query string) (wire.PreparedStatementFn, []oid.Oid, wire.Columns, error) {
	// Get connection ID from context
	connectionID := getConnectionIDSimple(ctx)

	// Create a prepared statement function
	fn := func(ctx context.Context, writer wire.DataWriter, parameters []string) error {
		return h.executeQuerySimple(ctx, connectionID, query, writer)
	}

	return fn, nil, nil, nil
}

// executeQuerySimple executes queries
func (h *SimpleWireHandler) executeQuerySimple(ctx context.Context, connectionID string, query string, writer wire.DataWriter) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	// Log query
	log.Printf("DEBUG: Executing query for connection %s: %s", connectionID, truncateQuerySimple(query))

	// Record query in monitor if in transaction
	h.txMonitor.RecordQuery(connectionID)

	// Handle transaction control statements
	upperQuery := strings.ToUpper(query)
	switch {
	case strings.HasPrefix(upperQuery, "BEGIN"):
		return h.handleBeginSimple(connectionID, query)
	case strings.HasPrefix(upperQuery, "COMMIT"):
		return h.handleCommitSimple(connectionID)
	case strings.HasPrefix(upperQuery, "ROLLBACK"):
		return h.handleRollbackSimple(connectionID)
	case strings.HasPrefix(upperQuery, "SELECT"):
		return h.handleSelectSimple(ctx, connectionID, query)
	case strings.HasPrefix(upperQuery, "INSERT"), strings.HasPrefix(upperQuery, "UPDATE"), strings.HasPrefix(upperQuery, "DELETE"):
		return h.handleDMLSimple(ctx, connectionID, query)
	case strings.HasPrefix(upperQuery, "CREATE"), strings.HasPrefix(upperQuery, "DROP"), strings.HasPrefix(upperQuery, "ALTER"):
		return h.handleDDLSimple(ctx, connectionID, query)
	default:
		return h.handleGenericSimple(ctx, connectionID, query)
	}
}

func (h *SimpleWireHandler) handleBeginSimple(connectionID string, query string) error {
	mode := "deferred"
	upperQuery := strings.ToUpper(query)
	if strings.Contains(upperQuery, "IMMEDIATE") {
		mode = "immediate"
	} else if strings.Contains(upperQuery, "EXCLUSIVE") {
		mode = "exclusive"
	}

	if err := h.txManager.Begin(connectionID, mode); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	h.txMonitor.StartTransaction(connectionID)
	return nil
}

func (h *SimpleWireHandler) handleCommitSimple(connectionID string) error {
	if err := h.txManager.Commit(connectionID); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	h.txMonitor.EndTransaction(connectionID, true)
	return nil
}

func (h *SimpleWireHandler) handleRollbackSimple(connectionID string) error {
	if err := h.txManager.Rollback(connectionID); err != nil {
		log.Printf("WARN: Rollback error for connection %s: %v", connectionID, err)
	}

	h.txMonitor.EndTransaction(connectionID, false)
	return nil
}

func (h *SimpleWireHandler) handleSelectSimple(ctx context.Context, connectionID string, query string) error {
	rows, err := h.backend.Query(connectionID, query)
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	// Just iterate through rows (actual wire protocol writing would be done by psql-wire)
	for rows.Next() {
		// Skip for now
	}

	return rows.Err()
}

func (h *SimpleWireHandler) handleDMLSimple(ctx context.Context, connectionID string, query string) error {
	_, err := h.backend.Exec(connectionID, query)
	if err != nil {
		return fmt.Errorf("exec error: %w", err)
	}
	return nil
}

func (h *SimpleWireHandler) handleDDLSimple(ctx context.Context, connectionID string, query string) error {
	_, err := h.backend.Exec(connectionID, query)
	if err != nil {
		return fmt.Errorf("ddl error: %w", err)
	}
	return nil
}

func (h *SimpleWireHandler) handleGenericSimple(ctx context.Context, connectionID string, query string) error {
	// Try as query first
	rows, err := h.backend.Query(connectionID, query)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			// Skip
		}
		return rows.Err()
	}

	// Try as exec
	_, err = h.backend.Exec(connectionID, query)
	return err
}

func getConnectionIDSimple(ctx context.Context) string {
	username := wire.AuthenticatedUsername(ctx)
	if username != "" {
		return username
	}
	return "unknown"
}

func truncateQuerySimple(query string) string {
	if len(query) > 100 {
		return query[:100] + "..."
	}
	return query
}
