// Package ledger manages account balances and validates transactions.
// It maintains an in-memory balance map rebuilt from the chain and provides
// persistence by saving/loading to a JSON file on disk.
package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Bihan293/Noda/chain"
	"github.com/Bihan293/Noda/crypto"
)

// StorageFile is the default file used to persist chain and balances.
const StorageFile = "node_data.json"

// Ledger holds the full blockchain, the balance map, and a mutex for safe concurrency.
type Ledger struct {
	Chain    *chain.Blockchain  `json:"chain"`
	Balances map[string]float64 `json:"balances"`
	mu       sync.RWMutex
	filePath string
}

// NewLedger creates a new ledger with a genesis chain and initial balances.
func NewLedger(filePath string) *Ledger {
	l := &Ledger{
		Chain:    chain.NewBlockchain(),
		Balances: make(map[string]float64),
		filePath: filePath,
	}
	// Genesis gives all coins to the genesis address.
	l.Balances[chain.GenesisAddress] = chain.GenesisSupply
	return l
}

// GetBalance returns the balance for a given address (0 if unknown).
func (l *Ledger) GetBalance(address string) float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.Balances[address]
}

// GetAllBalances returns a copy of the balances map (safe for serialization).
func (l *Ledger) GetAllBalances() map[string]float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	cp := make(map[string]float64, len(l.Balances))
	for k, v := range l.Balances {
		cp[k] = v
	}
	return cp
}

// ProcessTransaction validates and applies a transaction to the ledger.
// Validation rules:
//  1. Amount must be positive.
//  2. From and To addresses must be provided.
//  3. The signature must be valid (Ed25519 over "from:to:amount").
//  4. The sender must have sufficient balance.
//  5. No new coins are created (conservation check).
func (l *Ledger) ProcessTransaction(tx chain.Transaction) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// --- Basic validation ---
	if tx.Amount <= 0 {
		return fmt.Errorf("amount must be positive, got %f", tx.Amount)
	}
	if tx.From == "" || tx.To == "" {
		return fmt.Errorf("from and to addresses are required")
	}
	if tx.From == tx.To {
		return fmt.Errorf("cannot send to yourself")
	}

	// --- Signature verification ---
	msg := fmt.Sprintf("%s:%s:%f", tx.From, tx.To, tx.Amount)
	if !crypto.Verify(tx.From, []byte(msg), tx.Signature) {
		return fmt.Errorf("invalid signature")
	}

	// --- Balance check ---
	senderBalance := l.Balances[tx.From]
	if senderBalance < tx.Amount {
		return fmt.Errorf("insufficient balance: have %f, need %f", senderBalance, tx.Amount)
	}

	// --- Apply transfer ---
	tx.Timestamp = time.Now().Unix()
	l.Chain.AddTransaction(tx)
	l.Balances[tx.From] -= tx.Amount
	l.Balances[tx.To] += tx.Amount

	// Persist after every successful transaction.
	_ = l.saveLocked()

	return nil
}

// ReplaceChain replaces the current chain if the new one is longer and valid.
// This implements the "longest chain rule" for simple consensus.
func (l *Ledger) ReplaceChain(newChain *chain.Blockchain) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if newChain.Len() <= l.Chain.Len() {
		return false // new chain is not longer
	}

	// Validate the new chain and rebuild balances from scratch.
	balances, err := rebuildBalances(newChain)
	if err != nil {
		return false
	}

	l.Chain = newChain
	l.Balances = balances
	_ = l.saveLocked()
	return true
}

// rebuildBalances replays all transactions to compute account balances.
// Returns an error if any transaction (post-genesis) would create coins or overdraw.
func rebuildBalances(bc *chain.Blockchain) (map[string]float64, error) {
	balances := make(map[string]float64)

	for i, tx := range bc.Transactions {
		if i == 0 {
			// Genesis — credit the supply.
			balances[tx.To] += tx.Amount
			continue
		}
		// Post-genesis: no coin creation allowed.
		if tx.From == "" {
			return nil, fmt.Errorf("tx %d: coin creation outside genesis", i)
		}
		if balances[tx.From] < tx.Amount {
			return nil, fmt.Errorf("tx %d: insufficient balance", i)
		}
		balances[tx.From] -= tx.Amount
		balances[tx.To] += tx.Amount
	}
	return balances, nil
}

// Save persists the ledger to its JSON file (thread-safe wrapper).
func (l *Ledger) Save() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.saveLocked()
}

// saveLocked writes chain + balances to disk. Must be called with lock held.
func (l *Ledger) saveLocked() error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(l.filePath, data, 0644)
}

// LoadLedger reads a ledger from a JSON file. Falls back to a new ledger on failure.
func LoadLedger(filePath string) *Ledger {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return NewLedger(filePath)
	}

	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		return NewLedger(filePath)
	}
	l.filePath = filePath
	if l.Balances == nil {
		l.Balances = make(map[string]float64)
	}
	return &l
}

// GetChain returns a pointer to the underlying blockchain.
func (l *Ledger) GetChain() *chain.Blockchain {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.Chain
}
