// Package utxo implements an Unspent Transaction Output set.
//
// The UTXO set tracks every unspent output in the blockchain. Each output is
// identified by a composite key (txID + output index). When a transaction
// consumes an output, it is marked as spent and removed from the set.
//
// The UTXO model enables:
//   - Efficient balance lookups (sum of unspent outputs for an address)
//   - Double-spend detection (an output can only be spent once)
//   - Rebuilding the full set from the blockchain
//   - Thread-safe concurrent access
package utxo

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/Bihan293/Noda/block"
)

// ──────────────────────────────────────────────────────────────────────────────
// Types
// ──────────────────────────────────────────────────────────────────────────────

// OutPoint uniquely identifies a transaction output.
type OutPoint struct {
	TxID  string `json:"tx_id"`  // transaction hash
	Index int    `json:"index"`  // output index within the transaction
}

// String returns a human-readable representation of the outpoint.
func (op OutPoint) String() string {
	return fmt.Sprintf("%s:%d", op.TxID, op.Index)
}

// Key returns a string key for map lookups.
func (op OutPoint) Key() string {
	return fmt.Sprintf("%s:%d", op.TxID, op.Index)
}

// Output represents a single unspent transaction output.
type Output struct {
	Address string  `json:"address"` // owner address (public key hex)
	Amount  float64 `json:"amount"`  // coin amount
}

// ──────────────────────────────────────────────────────────────────────────────
// UTXO Set
// ──────────────────────────────────────────────────────────────────────────────

// Set is the main UTXO set — a map from OutPoint keys to Outputs.
type Set struct {
	utxos map[string]*utxoEntry // key = OutPoint.Key()
	mu    sync.RWMutex
}

// utxoEntry pairs an OutPoint with its Output for internal tracking.
type utxoEntry struct {
	OutPoint OutPoint `json:"outpoint"`
	Output   Output   `json:"output"`
}

// NewSet creates an empty UTXO set.
func NewSet() *Set {
	return &Set{
		utxos: make(map[string]*utxoEntry),
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Add / Spend
// ──────────────────────────────────────────────────────────────────────────────

// Add inserts an unspent output into the set.
func (s *Set) Add(op OutPoint, out Output) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.utxos[op.Key()] = &utxoEntry{
		OutPoint: op,
		Output:   out,
	}
}

// Spend removes an output from the set (marks it as spent).
// Returns the spent output, or an error if the output doesn't exist (double-spend).
func (s *Set) Spend(op OutPoint) (*Output, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := op.Key()
	entry, exists := s.utxos[key]
	if !exists {
		return nil, fmt.Errorf("utxo not found: %s (possible double-spend)", key)
	}

	out := entry.Output
	delete(s.utxos, key)
	return &out, nil
}

// Has checks whether an output exists in the UTXO set.
func (s *Set) Has(op OutPoint) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.utxos[op.Key()]
	return exists
}

// Get returns the output at the given outpoint, or nil if not found.
func (s *Set) Get(op OutPoint) *Output {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, exists := s.utxos[op.Key()]
	if !exists {
		return nil
	}
	out := entry.Output
	return &out
}

// ──────────────────────────────────────────────────────────────────────────────
// Balance Queries
// ──────────────────────────────────────────────────────────────────────────────

// Balance returns the total unspent amount for a given address.
func (s *Set) Balance(address string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total float64
	for _, entry := range s.utxos {
		if entry.Output.Address == address {
			total += entry.Output.Amount
		}
	}
	return total
}

// AllBalances returns a map of address -> total unspent balance for all addresses.
func (s *Set) AllBalances() map[string]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	balances := make(map[string]float64)
	for _, entry := range s.utxos {
		balances[entry.Output.Address] += entry.Output.Amount
	}
	return balances
}

// GetUTXOsForAddress returns all unspent outputs belonging to the given address.
func (s *Set) GetUTXOsForAddress(address string) []struct {
	OutPoint OutPoint
	Output   Output
} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []struct {
		OutPoint OutPoint
		Output   Output
	}
	for _, entry := range s.utxos {
		if entry.Output.Address == address {
			result = append(result, struct {
				OutPoint OutPoint
				Output   Output
			}{
				OutPoint: entry.OutPoint,
				Output:   entry.Output,
			})
		}
	}
	return result
}

// FindUTXOsForAmount finds enough UTXOs from the given address to cover the
// requested amount. Returns the selected UTXOs and the total value.
// Returns an error if insufficient funds.
func (s *Set) FindUTXOsForAmount(address string, amount float64) ([]OutPoint, float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var selected []OutPoint
	var total float64

	for _, entry := range s.utxos {
		if entry.Output.Address != address {
			continue
		}
		selected = append(selected, entry.OutPoint)
		total += entry.Output.Amount
		if total >= amount {
			return selected, total, nil
		}
	}

	if total < amount {
		return nil, total, fmt.Errorf("insufficient funds: address %s has %.6f, needs %.6f",
			shortAddr(address), total, amount)
	}
	return selected, total, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Size & Stats
// ──────────────────────────────────────────────────────────────────────────────

// Size returns the number of unspent outputs in the set.
func (s *Set) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.utxos)
}

// ──────────────────────────────────────────────────────────────────────────────
// Block Processing — Apply / Rollback
// ──────────────────────────────────────────────────────────────────────────────

// ApplyBlock processes all transactions in a block, updating the UTXO set.
// For each transaction:
//   - Coinbase/genesis (From==""): only creates outputs, no inputs to consume
//   - Regular: spends the sender's outputs and creates new outputs
//
// In our current account-like transaction model (single From→To with amount),
// each transaction produces one output to the recipient. If change is needed
// (sender had more than the amount), it creates a change output back to sender.
//
// For simplicity in this stage, we treat each transaction as consuming all UTXOs
// from the sender that are needed to cover the amount, and producing:
//   1. An output for the recipient (amount)
//   2. A change output for the sender (if any)
func (s *Set) ApplyBlock(b *block.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, tx := range b.Transactions {
		if tx.From == "" {
			// Coinbase / genesis transaction: create output for recipient.
			op := OutPoint{TxID: tx.ID, Index: 0}
			s.utxos[op.Key()] = &utxoEntry{
				OutPoint: op,
				Output: Output{
					Address: tx.To,
					Amount:  tx.Amount,
				},
			}
			continue
		}

		// Regular transaction: find and consume sender's UTXOs.
		// Collect enough UTXOs from sender to cover the amount.
		var senderUTXOs []string // keys
		var senderTotal float64

		for key, entry := range s.utxos {
			if entry.Output.Address == tx.From {
				senderUTXOs = append(senderUTXOs, key)
				senderTotal += entry.Output.Amount
				if senderTotal >= tx.Amount {
					break
				}
			}
		}

		if senderTotal < tx.Amount {
			return fmt.Errorf("block %d, tx %d: insufficient UTXOs for %s (has %.6f, needs %.6f)",
				b.Header.Height, i, shortAddr(tx.From), senderTotal, tx.Amount)
		}

		// Remove consumed UTXOs.
		for _, key := range senderUTXOs {
			delete(s.utxos, key)
		}

		// Create output for recipient.
		outRecipient := OutPoint{TxID: tx.ID, Index: 0}
		s.utxos[outRecipient.Key()] = &utxoEntry{
			OutPoint: outRecipient,
			Output: Output{
				Address: tx.To,
				Amount:  tx.Amount,
			},
		}

		// Create change output for sender (if needed).
		change := senderTotal - tx.Amount
		if change > 0.00000001 { // avoid dust
			outChange := OutPoint{TxID: tx.ID, Index: 1}
			s.utxos[outChange.Key()] = &utxoEntry{
				OutPoint: outChange,
				Output: Output{
					Address: tx.From,
					Amount:  change,
				},
			}
		}
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Rebuild from blockchain
// ──────────────────────────────────────────────────────────────────────────────

// RebuildFromBlocks rebuilds the entire UTXO set by replaying all blocks.
func RebuildFromBlocks(blocks []*block.Block) (*Set, error) {
	s := NewSet()
	for _, b := range blocks {
		if err := s.ApplyBlock(b); err != nil {
			return nil, fmt.Errorf("rebuild failed at block %d: %w", b.Header.Height, err)
		}
	}
	log.Printf("[UTXO] Rebuilt from %d blocks — %d unspent outputs", len(blocks), s.Size())
	return s, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Serialization
// ──────────────────────────────────────────────────────────────────────────────

// MarshalJSON serializes the UTXO set to JSON.
func (s *Set) MarshalJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]*utxoEntry, 0, len(s.utxos))
	for _, entry := range s.utxos {
		entries = append(entries, entry)
	}
	return json.Marshal(entries)
}

// UnmarshalJSON deserializes the UTXO set from JSON.
func (s *Set) UnmarshalJSON(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var entries []*utxoEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	s.utxos = make(map[string]*utxoEntry, len(entries))
	for _, entry := range entries {
		s.utxos[entry.OutPoint.Key()] = entry
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func shortAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-4:]
}
