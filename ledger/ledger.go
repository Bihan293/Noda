// Package ledger manages account balances and validates transactions.
// It maintains an in-memory balance map rebuilt from the chain and provides
// persistence by saving/loading to a JSON file on disk.
//
// The ledger also supports a faucet mode: a pre-funded wallet that can
// distribute small amounts of coins with a per-address cooldown.
package ledger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Bihan293/Noda/chain"
	"github.com/Bihan293/Noda/crypto"
)

// StorageFile is the default file used to persist chain and balances.
const StorageFile = "node_data.json"

// FaucetAmount is how many coins the faucet distributes per request.
const FaucetAmount = 50.0

// FaucetCooldown is the minimum time between faucet requests for the same address.
const FaucetCooldown = 60 * time.Second

// Ledger holds the full blockchain, the balance map, and a mutex for safe concurrency.
type Ledger struct {
	Chain    *chain.Blockchain  `json:"chain"`
	Balances map[string]float64 `json:"balances"`
	mu       sync.RWMutex
	filePath string

	// Faucet state (not persisted — resets on restart).
	faucetPrivKey  string            // hex-encoded Ed25519 private key for faucet wallet
	faucetAddress  string            // derived from faucetPrivKey
	faucetCooldown map[string]time.Time // address -> last faucet claim time
}

// NewLedger creates a new ledger with a genesis chain and initial balances.
func NewLedger(filePath string) *Ledger {
	l := &Ledger{
		Chain:          chain.NewBlockchain(),
		Balances:       make(map[string]float64),
		filePath:       filePath,
		faucetCooldown: make(map[string]time.Time),
	}
	// Genesis gives all coins to the genesis address.
	l.Balances[chain.GenesisAddress] = chain.GenesisSupply
	log.Printf("[LEDGER] New ledger created — genesis supply: %d coins at %s",
		chain.GenesisSupply, chain.GenesisAddress)
	return l
}

// SetFaucetKey configures the faucet wallet from a hex-encoded private key.
// The address is derived automatically. Call this before starting the server.
func (l *Ledger) SetFaucetKey(privKeyHex string) error {
	addr, err := crypto.AddressFromPrivateKey(privKeyHex)
	if err != nil {
		return fmt.Errorf("invalid faucet private key: %w", err)
	}
	l.faucetPrivKey = privKeyHex
	l.faucetAddress = addr
	log.Printf("[FAUCET] Faucet wallet configured — address: %s", addr)
	return nil
}

// FaucetAddress returns the faucet wallet address (empty if not configured).
func (l *Ledger) FaucetAddress() string {
	return l.faucetAddress
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
//
// Validation rules:
//  1. Amount must be positive.
//  2. From and To addresses must be non-empty.
//  3. Sender and receiver must differ.
//  4. The signature must be valid (Ed25519 over "from:to:amount").
//  5. The sender must have sufficient balance.
func (l *Ledger) ProcessTransaction(tx chain.Transaction) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// --- Basic field validation ---
	if tx.Amount <= 0 {
		log.Printf("[TX REJECTED] invalid amount %.6f from %s", tx.Amount, shortAddr(tx.From))
		return fmt.Errorf("invalid amount: must be positive, got %f", tx.Amount)
	}
	if tx.From == "" || tx.To == "" {
		log.Printf("[TX REJECTED] missing address — from=%q to=%q", tx.From, tx.To)
		return fmt.Errorf("invalid addresses: both 'from' and 'to' are required")
	}
	if tx.From == tx.To {
		log.Printf("[TX REJECTED] self-send from %s", shortAddr(tx.From))
		return fmt.Errorf("invalid transaction: cannot send to yourself")
	}

	// --- Signature verification ---
	msg := fmt.Sprintf("%s:%s:%f", tx.From, tx.To, tx.Amount)
	if !crypto.Verify(tx.From, []byte(msg), tx.Signature) {
		log.Printf("[TX REJECTED] invalid signature from %s -> %s (%.2f coins)",
			shortAddr(tx.From), shortAddr(tx.To), tx.Amount)
		return fmt.Errorf("invalid signature: Ed25519 verification failed for sender %s", shortAddr(tx.From))
	}

	// --- Balance check ---
	senderBalance := l.Balances[tx.From]
	if senderBalance < tx.Amount {
		log.Printf("[TX REJECTED] insufficient balance: %s has %.2f, needs %.2f",
			shortAddr(tx.From), senderBalance, tx.Amount)
		return fmt.Errorf("insufficient balance: address %s has %.6f coins, tried to send %.6f",
			shortAddr(tx.From), senderBalance, tx.Amount)
	}

	// --- Apply transfer ---
	tx.Timestamp = time.Now().Unix()
	l.Chain.AddTransaction(tx)
	l.Balances[tx.From] -= tx.Amount
	l.Balances[tx.To] += tx.Amount

	log.Printf("[TX ACCEPTED] %s -> %s : %.2f coins (chain length: %d)",
		shortAddr(tx.From), shortAddr(tx.To), tx.Amount, l.Chain.Len())

	// Persist after every successful transaction.
	_ = l.saveLocked()

	return nil
}

// ProcessFaucet sends FaucetAmount coins from the faucet wallet to the given address.
// It enforces a per-address cooldown to prevent abuse.
func (l *Ledger) ProcessFaucet(toAddress string) (*chain.Transaction, error) {
	// Check faucet is configured.
	if l.faucetPrivKey == "" || l.faucetAddress == "" {
		return nil, fmt.Errorf("faucet not configured: start node with -faucet-key flag")
	}

	if toAddress == "" {
		return nil, fmt.Errorf("invalid address: 'to' address is required")
	}
	if toAddress == l.faucetAddress {
		return nil, fmt.Errorf("invalid address: cannot send faucet coins to the faucet itself")
	}

	// Check cooldown (outside main lock to avoid holding it too long).
	l.mu.Lock()
	lastClaim, exists := l.faucetCooldown[toAddress]
	if exists && time.Since(lastClaim) < FaucetCooldown {
		remaining := FaucetCooldown - time.Since(lastClaim)
		l.mu.Unlock()
		log.Printf("[FAUCET REJECTED] cooldown active for %s (%.0fs remaining)",
			shortAddr(toAddress), remaining.Seconds())
		return nil, fmt.Errorf("faucet cooldown: try again in %.0f seconds", remaining.Seconds())
	}
	l.mu.Unlock()

	// Sign the transaction.
	sig, err := crypto.SignTransaction(l.faucetPrivKey, l.faucetAddress, toAddress, FaucetAmount)
	if err != nil {
		return nil, fmt.Errorf("faucet signing failed: %w", err)
	}

	tx := chain.Transaction{
		From:      l.faucetAddress,
		To:        toAddress,
		Amount:    FaucetAmount,
		Signature: sig,
	}

	// Process through normal validation.
	if err := l.ProcessTransaction(tx); err != nil {
		return nil, fmt.Errorf("faucet transaction failed: %w", err)
	}

	// Record cooldown.
	l.mu.Lock()
	l.faucetCooldown[toAddress] = time.Now()
	l.mu.Unlock()

	log.Printf("[FAUCET] Sent %.2f coins to %s", FaucetAmount, shortAddr(toAddress))
	return &tx, nil
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
		log.Printf("[SYNC REJECTED] invalid chain: %v", err)
		return false
	}

	l.Chain = newChain
	l.Balances = balances
	_ = l.saveLocked()

	log.Printf("[SYNC] Chain replaced — new length: %d", newChain.Len())
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
		log.Printf("[LEDGER] No data file found at %s — starting fresh", filePath)
		return NewLedger(filePath)
	}

	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		log.Printf("[LEDGER] Failed to parse %s: %v — starting fresh", filePath, err)
		return NewLedger(filePath)
	}
	l.filePath = filePath
	l.faucetCooldown = make(map[string]time.Time)
	if l.Balances == nil {
		l.Balances = make(map[string]float64)
	}

	log.Printf("[LEDGER] Loaded %d transactions from %s", l.Chain.Len(), filePath)
	return &l
}

// GetChain returns a pointer to the underlying blockchain.
func (l *Ledger) GetChain() *chain.Blockchain {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.Chain
}

// shortAddr returns the first 8 and last 4 chars of an address for logging.
func shortAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-4:]
}
