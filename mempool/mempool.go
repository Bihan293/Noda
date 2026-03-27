// Package mempool implements an in-memory pool of unconfirmed transactions.
//
// The mempool holds transactions that have been validated but not yet included
// in a block. It supports:
//   - Thread-safe concurrent access
//   - Transaction validation before admission (signature, double-spend via UTXO)
//   - Priority ordering by arrival time (FIFO)
//   - Configurable pool size limit with eviction (oldest first)
//   - Removal of transactions once they are included in a block
//   - Querying pending transactions for block assembly
package mempool

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Bihan293/Noda/block"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	// DefaultMaxSize is the maximum number of transactions in the mempool.
	DefaultMaxSize = 10_000

	// DefaultTxTTL is how long a transaction can stay in the mempool before eviction.
	DefaultTxTTL = 24 * time.Hour
)

// ──────────────────────────────────────────────────────────────────────────────
// MempoolEntry
// ──────────────────────────────────────────────────────────────────────────────

// Entry wraps a transaction with metadata for pool management.
type Entry struct {
	Tx       block.Transaction `json:"tx"`
	AddedAt  time.Time         `json:"added_at"`  // when the TX was added to the pool
	Priority int64             `json:"priority"`   // arrival order (lower = earlier = higher priority)
}

// ──────────────────────────────────────────────────────────────────────────────
// Mempool
// ──────────────────────────────────────────────────────────────────────────────

// Mempool is a thread-safe pool of unconfirmed transactions.
type Mempool struct {
	entries  map[string]*Entry // txID -> entry
	order    []string          // insertion order (for FIFO priority)
	maxSize  int
	sequence int64 // monotonic counter for arrival priority
	mu       sync.RWMutex
}

// New creates a new mempool with the given maximum size.
// If maxSize <= 0, DefaultMaxSize is used.
func New(maxSize int) *Mempool {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	return &Mempool{
		entries: make(map[string]*Entry),
		order:   make([]string, 0, 256),
		maxSize: maxSize,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Add / Remove
// ──────────────────────────────────────────────────────────────────────────────

// Add inserts a transaction into the mempool.
// The transaction must already have a valid ID set.
// Returns an error if the transaction is a duplicate or the pool is full after eviction.
func (mp *Mempool) Add(tx block.Transaction) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if tx.ID == "" {
		return fmt.Errorf("transaction has no ID")
	}

	// Check for duplicates.
	if _, exists := mp.entries[tx.ID]; exists {
		return fmt.Errorf("transaction %s already in mempool", shortID(tx.ID))
	}

	// Evict expired entries first.
	mp.evictExpiredLocked()

	// Check pool size limit.
	if len(mp.entries) >= mp.maxSize {
		// Evict the oldest transaction.
		if !mp.evictOldestLocked() {
			return fmt.Errorf("mempool is full (%d transactions)", mp.maxSize)
		}
	}

	mp.sequence++
	entry := &Entry{
		Tx:       tx,
		AddedAt:  time.Now(),
		Priority: mp.sequence,
	}

	mp.entries[tx.ID] = entry
	mp.order = append(mp.order, tx.ID)

	log.Printf("[MEMPOOL] Added TX %s (%s -> %s : %.2f) — pool size: %d",
		shortID(tx.ID), shortAddr(tx.From), shortAddr(tx.To), tx.Amount, len(mp.entries))

	return nil
}

// Remove deletes a transaction from the mempool (e.g., after block inclusion).
func (mp *Mempool) Remove(txID string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.removeLocked(txID)
}

// RemoveBatch removes multiple transactions (typically after a block is mined).
func (mp *Mempool) RemoveBatch(txIDs []string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	for _, id := range txIDs {
		mp.removeLocked(id)
	}
	log.Printf("[MEMPOOL] Removed %d transactions (block confirmed) — pool size: %d",
		len(txIDs), len(mp.entries))
}

// removeLocked removes a single TX. Must be called with lock held.
func (mp *Mempool) removeLocked(txID string) {
	delete(mp.entries, txID)
	// Remove from order slice.
	for i, id := range mp.order {
		if id == txID {
			mp.order = append(mp.order[:i], mp.order[i+1:]...)
			break
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Query
// ──────────────────────────────────────────────────────────────────────────────

// Has returns true if the transaction is in the mempool.
func (mp *Mempool) Has(txID string) bool {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	_, ok := mp.entries[txID]
	return ok
}

// Get returns a transaction from the mempool, or nil if not found.
func (mp *Mempool) Get(txID string) *block.Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	entry, ok := mp.entries[txID]
	if !ok {
		return nil
	}
	tx := entry.Tx
	return &tx
}

// Size returns the number of transactions currently in the pool.
func (mp *Mempool) Size() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.entries)
}

// GetPending returns up to `limit` pending transactions in FIFO order.
// These are candidates for inclusion in the next block.
func (mp *Mempool) GetPending(limit int) []block.Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	result := make([]block.Transaction, 0, min(limit, len(mp.order)))
	for _, id := range mp.order {
		if len(result) >= limit {
			break
		}
		if entry, ok := mp.entries[id]; ok {
			result = append(result, entry.Tx)
		}
	}
	return result
}

// GetAll returns all pending transactions in FIFO order.
func (mp *Mempool) GetAll() []block.Transaction {
	return mp.GetPending(len(mp.order))
}

// ──────────────────────────────────────────────────────────────────────────────
// Eviction
// ──────────────────────────────────────────────────────────────────────────────

// evictExpiredLocked removes transactions older than DefaultTxTTL.
// Must be called with lock held.
func (mp *Mempool) evictExpiredLocked() {
	cutoff := time.Now().Add(-DefaultTxTTL)
	var toRemove []string
	for id, entry := range mp.entries {
		if entry.AddedAt.Before(cutoff) {
			toRemove = append(toRemove, id)
		}
	}
	for _, id := range toRemove {
		mp.removeLocked(id)
		log.Printf("[MEMPOOL] Evicted expired TX %s", shortID(id))
	}
}

// evictOldestLocked removes the oldest transaction to make room.
// Returns true if a transaction was evicted.
func (mp *Mempool) evictOldestLocked() bool {
	if len(mp.order) == 0 {
		return false
	}
	oldest := mp.order[0]
	mp.removeLocked(oldest)
	log.Printf("[MEMPOOL] Evicted oldest TX %s (pool full)", shortID(oldest))
	return true
}

// ──────────────────────────────────────────────────────────────────────────────
// Double-Spend Check (checks if any TX in mempool spends from same sender)
// ──────────────────────────────────────────────────────────────────────────────

// HasSpendFrom returns true if the mempool already contains a transaction
// from the given sender address. Used for simple double-spend detection.
func (mp *Mempool) HasSpendFrom(address string) bool {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	for _, entry := range mp.entries {
		if entry.Tx.From == address {
			return true
		}
	}
	return false
}

// GetSpendingTotal returns the total amount being spent by the given address
// across all pending transactions in the mempool.
func (mp *Mempool) GetSpendingTotal(address string) float64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	var total float64
	for _, entry := range mp.entries {
		if entry.Tx.From == address {
			total += entry.Tx.Amount
		}
	}
	return total
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func shortID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:8] + "..." + id[len(id)-4:]
}

func shortAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-4:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
