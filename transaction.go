package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// TransactionManager manages transaction lifecycle and synchronization with blob storage
type TransactionManager struct {
	backend       *SQLiteBackend
	cache         *DatabaseCache
	mu            sync.Mutex
	lastUpload    time.Time
	uploadPending bool
	uploadChan    chan bool
	stopChan      chan bool
	wg            sync.WaitGroup
}

// NewTransactionManager creates a new transaction manager
func NewTransactionManager(backend *SQLiteBackend, cache *DatabaseCache) *TransactionManager {
	tm := &TransactionManager{
		backend:    backend,
		cache:      cache,
		uploadChan: make(chan bool, 1),
		stopChan:   make(chan bool),
	}

	// Start background upload worker
	tm.wg.Add(1)
	go tm.uploadWorker()

	return tm
}

// uploadWorker handles background uploads to blob storage
func (tm *TransactionManager) uploadWorker() {
	defer tm.wg.Done()

	ticker := time.NewTicker(time.Duration(tm.cache.ttlMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-tm.stopChan:
			// Final upload before stopping
			tm.performUpload()
			return

		case <-tm.uploadChan:
			// Upload requested after transaction commit
			tm.performUpload()

		case <-ticker.C:
			// Periodic upload check
			tm.mu.Lock()
			if tm.uploadPending || tm.cache.ShouldSync() {
				tm.mu.Unlock()
				tm.performUpload()
			} else {
				tm.mu.Unlock()
			}
		}
	}
}

// performUpload uploads the database to blob storage
func (tm *TransactionManager) performUpload() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := tm.cache.Upload(ctx); err != nil {
		log.Printf("ERROR: Failed to upload database to blob storage: %v", err)
		return
	}

	log.Printf("INFO: Successfully uploaded database to blob storage")
	tm.uploadPending = false
	tm.lastUpload = time.Now()
}

// Begin starts a new transaction
func (tm *TransactionManager) Begin(connectionID string, mode string) error {
	return tm.backend.BeginTransaction(connectionID, mode)
}

// Commit commits a transaction and triggers upload to blob storage
func (tm *TransactionManager) Commit(connectionID string) error {
	if err := tm.backend.CommitTransaction(connectionID); err != nil {
		return err
	}

	// Mark that we need to upload and trigger async upload
	tm.mu.Lock()
	tm.uploadPending = true
	tm.mu.Unlock()

	// Signal upload worker (non-blocking)
	select {
	case tm.uploadChan <- true:
	default:
	}

	return nil
}

// Rollback rolls back a transaction
func (tm *TransactionManager) Rollback(connectionID string) error {
	return tm.backend.RollbackTransaction(connectionID)
}

// ForceUpload forces an immediate upload to blob storage
func (tm *TransactionManager) ForceUpload(ctx context.Context) error {
	if err := tm.cache.Upload(ctx); err != nil {
		return fmt.Errorf("failed to force upload: %w", err)
	}

	tm.mu.Lock()
	tm.uploadPending = false
	tm.lastUpload = time.Now()
	tm.mu.Unlock()

	return nil
}

// Stop stops the transaction manager and performs final upload
func (tm *TransactionManager) Stop() {
	close(tm.stopChan)
	tm.wg.Wait()
}

// GetTransactionStatus returns the current transaction status for a connection
func (tm *TransactionManager) GetTransactionStatus(connectionID string) TransactionStatus {
	return tm.backend.GetTransactionStatus(connectionID)
}

// TransactionContext holds information about a transaction
type TransactionContext struct {
	ConnectionID string
	StartTime    time.Time
	LastQuery    time.Time
	QueryCount   int
	InTransaction bool
}

// TransactionMonitor monitors and tracks transaction metrics
type TransactionMonitor struct {
	mu           sync.RWMutex
	transactions map[string]*TransactionContext
	metrics      *TransactionMetrics
}

// TransactionMetrics holds transaction-related metrics
type TransactionMetrics struct {
	mu                sync.RWMutex
	TotalTransactions int64
	CommittedTx       int64
	RolledBackTx      int64
	ActiveTx          int64
	AvgTxDuration     time.Duration
	LongestTx         time.Duration
}

// NewTransactionMonitor creates a new transaction monitor
func NewTransactionMonitor() *TransactionMonitor {
	return &TransactionMonitor{
		transactions: make(map[string]*TransactionContext),
		metrics:      &TransactionMetrics{},
	}
}

// StartTransaction records the start of a transaction
func (tm *TransactionMonitor) StartTransaction(connectionID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	ctx := &TransactionContext{
		ConnectionID:  connectionID,
		StartTime:     time.Now(),
		LastQuery:     time.Now(),
		InTransaction: true,
	}
	tm.transactions[connectionID] = ctx

	tm.metrics.mu.Lock()
	tm.metrics.TotalTransactions++
	tm.metrics.ActiveTx++
	tm.metrics.mu.Unlock()
}

// EndTransaction records the end of a transaction
func (tm *TransactionMonitor) EndTransaction(connectionID string, committed bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	ctx, exists := tm.transactions[connectionID]
	if !exists {
		return
	}

	duration := time.Since(ctx.StartTime)

	tm.metrics.mu.Lock()
	if committed {
		tm.metrics.CommittedTx++
	} else {
		tm.metrics.RolledBackTx++
	}
	tm.metrics.ActiveTx--

	// Update average duration
	totalTx := tm.metrics.CommittedTx + tm.metrics.RolledBackTx
	if totalTx > 0 {
		tm.metrics.AvgTxDuration = (tm.metrics.AvgTxDuration*time.Duration(totalTx-1) + duration) / time.Duration(totalTx)
	}

	// Update longest transaction
	if duration > tm.metrics.LongestTx {
		tm.metrics.LongestTx = duration
	}
	tm.metrics.mu.Unlock()

	delete(tm.transactions, connectionID)
}

// RecordQuery records a query execution in a transaction
func (tm *TransactionMonitor) RecordQuery(connectionID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if ctx, exists := tm.transactions[connectionID]; exists {
		ctx.LastQuery = time.Now()
		ctx.QueryCount++
	}
}

// GetMetrics returns a copy of current transaction metrics
func (tm *TransactionMonitor) GetMetrics() TransactionMetrics {
	tm.metrics.mu.RLock()
	defer tm.metrics.mu.RUnlock()

	return TransactionMetrics{
		TotalTransactions: tm.metrics.TotalTransactions,
		CommittedTx:       tm.metrics.CommittedTx,
		RolledBackTx:      tm.metrics.RolledBackTx,
		ActiveTx:          tm.metrics.ActiveTx,
		AvgTxDuration:     tm.metrics.AvgTxDuration,
		LongestTx:         tm.metrics.LongestTx,
	}
}

// GetActiveTransactions returns information about all active transactions
func (tm *TransactionMonitor) GetActiveTransactions() []TransactionContext {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var active []TransactionContext
	for _, ctx := range tm.transactions {
		if ctx.InTransaction {
			active = append(active, *ctx)
		}
	}
	return active
}

// CheckStaleTransactions checks for transactions that have been idle too long
func (tm *TransactionMonitor) CheckStaleTransactions(maxIdleTime time.Duration) []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var stale []string
	now := time.Now()

	for connID, ctx := range tm.transactions {
		if ctx.InTransaction && now.Sub(ctx.LastQuery) > maxIdleTime {
			stale = append(stale, connID)
		}
	}

	return stale
}
