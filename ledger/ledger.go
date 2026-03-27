// Package ledger manages account balances and validates transactions.
// It maintains an in-memory balance map rebuilt from the blockchain and provides
// persistence by saving/loading to a JSON file on disk.
//
// The ledger supports:
//   - Block-based transaction processing
//   - Faucet: 5,000 coins per request, global cap 11,000,000 total
//   - Mining rewards with halving
//   - Balance tracking from block transactions
package ledger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Bihan293/Noda/block"
	"github.com/Bihan293/Noda/chain"
	"github.com/Bihan293/Noda/crypto"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	// StorageFile is the default file used to persist chain and balances.
	StorageFile = "node_data.json"

	// FaucetAmount is how many coins the faucet distributes per request.
	// Updated from 50 → 5,000 as per new tokenomics.
	FaucetAmount = 5000.0

	// FaucetGlobalCap is the maximum total coins that can be distributed via faucet.
	FaucetGlobalCap = 11_000_000.0

	// FaucetCooldown is the minimum time between faucet requests for the same address.
	FaucetCooldown = 60 * time.Second
)

// ──────────────────────────────────────────────────────────────────────────────
// Ledger
// ──────────────────────────────────────────────────────────────────────────────

// Ledger holds the full blockchain, the balance map, and state for faucet/mining.
type Ledger struct {
	Chain    *chain.Blockchain  `json:"chain"`
	Balances map[string]float64 `json:"balances"`
	mu       sync.RWMutex
	filePath string

	// Faucet state
	faucetPrivKey  string            // hex-encoded Ed25519 private key for faucet wallet
	faucetAddress  string            // derived from faucetPrivKey
	faucetCooldown map[string]time.Time // address -> last faucet claim time
}

// NewLedger creates a new ledger with a genesis blockchain and initial balances.
func NewLedger(filePath string) *Ledger {
	l := &Ledger{
		Chain:          chain.NewBlockchain(),
		Balances:       make(map[string]float64),
		filePath:       filePath,
		faucetCooldown: make(map[string]time.Time),
	}
	// Genesis gives all coins to the genesis address.
	l.Balances[block.GenesisAddress] = block.GenesisSupply
	log.Printf("[LEDGER] New ledger created — genesis supply: %.0f coins at %s",
		block.GenesisSupply, block.GenesisAddress)
	return l
}

// ──────────────────────────────────────────────────────────────────────────────
// Faucet Configuration
// ──────────────────────────────────────────────────────────────────────────────

// SetFaucetKey configures the faucet wallet from a hex-encoded private key.
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

// FaucetTotalDistributed returns total coins distributed via faucet.
func (l *Ledger) FaucetTotalDistributed() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.Chain.TotalFaucet
}

// IsFaucetActive returns true if the faucet can still distribute coins.
func (l *Ledger) IsFaucetActive() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.faucetPrivKey != "" && l.Chain.TotalFaucet < FaucetGlobalCap
}

// FaucetRemaining returns how many coins the faucet can still distribute.
func (l *Ledger) FaucetRemaining() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	remaining := FaucetGlobalCap - l.Chain.TotalFaucet
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ──────────────────────────────────────────────────────────────────────────────
// Balance Queries
// ──────────────────────────────────────────────────────────────────────────────

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

// ──────────────────────────────────────────────────────────────────────────────
// Transaction Processing (within blocks)
// ──────────────────────────────────────────────────────────────────────────────

// ProcessTransaction validates a single transaction and applies it to balances.
// This is used for transactions within a block (not standalone flat transactions).
//
// Validation rules:
//  1. Amount must be positive.
//  2. From and To addresses must be non-empty (except coinbase).
//  3. Sender and receiver must differ.
//  4. The signature must be valid (Ed25519 over "from:to:amount").
//  5. The sender must have sufficient balance.
func (l *Ledger) ProcessTransaction(tx block.Transaction) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.processTransactionLocked(tx)
}

// processTransactionLocked does the actual validation and balance update.
// Must be called with l.mu held.
func (l *Ledger) processTransactionLocked(tx block.Transaction) error {
	// --- Basic field validation ---
	if tx.Amount <= 0 {
		log.Printf("[TX REJECTED] invalid amount %.6f from %s", tx.Amount, shortAddr(tx.From))
		return fmt.Errorf("invalid amount: must be positive, got %f", tx.Amount)
	}

	// Coinbase transactions are handled separately.
	if tx.From == "" {
		// This is a coinbase or genesis — credit the recipient.
		l.Balances[tx.To] += tx.Amount
		return nil
	}

	if tx.To == "" {
		log.Printf("[TX REJECTED] missing 'to' address from %s", shortAddr(tx.From))
		return fmt.Errorf("invalid addresses: 'to' is required")
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
	l.Balances[tx.From] -= tx.Amount
	l.Balances[tx.To] += tx.Amount

	log.Printf("[TX ACCEPTED] %s -> %s : %.2f coins",
		shortAddr(tx.From), shortAddr(tx.To), tx.Amount)

	return nil
}

// ValidateAndProcessUserTx validates a user-submitted transaction (not coinbase),
// adds it as a single-tx block to the chain, and persists.
// This is the main entry point for POST /transaction and POST /send.
func (l *Ledger) ValidateAndProcessUserTx(tx block.Transaction) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Validate first (without applying).
	if tx.Amount <= 0 {
		return fmt.Errorf("invalid amount: must be positive, got %f", tx.Amount)
	}
	if tx.From == "" || tx.To == "" {
		return fmt.Errorf("invalid addresses: both 'from' and 'to' are required")
	}
	if tx.From == tx.To {
		return fmt.Errorf("invalid transaction: cannot send to yourself")
	}

	// Signature verification.
	msg := fmt.Sprintf("%s:%s:%f", tx.From, tx.To, tx.Amount)
	if !crypto.Verify(tx.From, []byte(msg), tx.Signature) {
		return fmt.Errorf("invalid signature: Ed25519 verification failed for sender %s", shortAddr(tx.From))
	}

	// Balance check.
	senderBalance := l.Balances[tx.From]
	if senderBalance < tx.Amount {
		return fmt.Errorf("insufficient balance: address %s has %.6f coins, tried to send %.6f",
			shortAddr(tx.From), senderBalance, tx.Amount)
	}

	// Set timestamp and compute ID.
	tx.Timestamp = time.Now().Unix()
	tx.ID = block.HashTransaction(tx)

	// Create a block containing this transaction.
	txIDs := []string{tx.ID}
	merkleRoot := block.ComputeMerkleRoot(txIDs)

	nextHeight := l.Chain.Height() + 1
	prevHash := l.Chain.LastHash()
	target := l.Chain.GetTarget()

	newBlock := &block.Block{
		Header: block.BlockHeader{
			Version:       block.BlockVersion,
			Height:        nextHeight,
			PrevBlockHash: prevHash,
			MerkleRoot:    merkleRoot,
			Timestamp:     time.Now().Unix(),
			Bits:          block.BitsFromTarget(target),
		},
		Transactions: []block.Transaction{tx},
	}

	// Mine the block (for user transactions, use a generous attempt limit).
	if err := block.MineBlock(newBlock, target, 10_000_000); err != nil {
		return fmt.Errorf("mining failed for user tx block: %w", err)
	}

	// Add block to chain.
	if err := l.Chain.AddBlock(newBlock); err != nil {
		return fmt.Errorf("failed to add block: %w", err)
	}

	// Apply balance changes.
	l.Balances[tx.From] -= tx.Amount
	l.Balances[tx.To] += tx.Amount

	log.Printf("[TX ACCEPTED] %s -> %s : %.2f coins (block height: %d)",
		shortAddr(tx.From), shortAddr(tx.To), tx.Amount, newBlock.Header.Height)

	// Persist.
	_ = l.saveLocked()
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Faucet
// ──────────────────────────────────────────────────────────────────────────────

// ProcessFaucet sends FaucetAmount coins from the faucet wallet to the given address.
// Enforces per-address cooldown and global faucet cap preparation.
func (l *Ledger) ProcessFaucet(toAddress string) (*block.Transaction, error) {
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

	// Check global faucet cap.
	l.mu.RLock()
	totalDistributed := l.Chain.TotalFaucet
	l.mu.RUnlock()

	if totalDistributed >= FaucetGlobalCap {
		return nil, fmt.Errorf("faucet exhausted: all %.0f coins have been distributed", FaucetGlobalCap)
	}

	// Calculate actual amount (may be less than FaucetAmount near the cap).
	amount := FaucetAmount
	remaining := FaucetGlobalCap - totalDistributed
	if amount > remaining {
		amount = remaining
	}

	// Check cooldown.
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
	sig, err := crypto.SignTransaction(l.faucetPrivKey, l.faucetAddress, toAddress, amount)
	if err != nil {
		return nil, fmt.Errorf("faucet signing failed: %w", err)
	}

	tx := block.Transaction{
		From:      l.faucetAddress,
		To:        toAddress,
		Amount:    amount,
		Signature: sig,
	}

	// Process through normal validation (creates a block).
	if err := l.ValidateAndProcessUserTx(tx); err != nil {
		return nil, fmt.Errorf("faucet transaction failed: %w", err)
	}

	// Update faucet tracking.
	l.mu.Lock()
	l.Chain.TotalFaucet += amount
	l.faucetCooldown[toAddress] = time.Now()
	l.mu.Unlock()

	log.Printf("[FAUCET] Sent %.0f coins to %s (total distributed: %.0f / %.0f)",
		amount, shortAddr(toAddress), totalDistributed+amount, FaucetGlobalCap)

	return &tx, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Chain Sync
// ──────────────────────────────────────────────────────────────────────────────

// ReplaceChain replaces the current chain if the new one is longer and valid.
func (l *Ledger) ReplaceChain(newChain *chain.Blockchain) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if newChain.Len() <= l.Chain.Len() {
		return false
	}

	// Validate the full chain.
	if err := chain.ValidateChain(newChain); err != nil {
		log.Printf("[SYNC REJECTED] invalid chain: %v", err)
		return false
	}

	// Rebuild balances from the new chain.
	balances, err := rebuildBalances(newChain)
	if err != nil {
		log.Printf("[SYNC REJECTED] balance rebuild failed: %v", err)
		return false
	}

	l.Chain = newChain
	l.Balances = balances
	_ = l.saveLocked()

	log.Printf("[SYNC] Chain replaced — new length: %d blocks", newChain.Len())
	return true
}

// rebuildBalances replays all block transactions to compute account balances.
func rebuildBalances(bc *chain.Blockchain) (map[string]float64, error) {
	balances := make(map[string]float64)

	for _, b := range bc.Blocks {
		for _, tx := range b.Transactions {
			if tx.From == "" {
				// Coinbase or genesis — credit the recipient.
				balances[tx.To] += tx.Amount
				continue
			}
			// Regular transaction.
			if balances[tx.From] < tx.Amount {
				return nil, fmt.Errorf("block %d: insufficient balance for %s",
					b.Header.Height, shortAddr(tx.From))
			}
			balances[tx.From] -= tx.Amount
			balances[tx.To] += tx.Amount
		}
	}
	return balances, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Persistence
// ──────────────────────────────────────────────────────────────────────────────

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

	// Rebuild chain target from hex if needed.
	if l.Chain != nil && l.Chain.Target == nil {
		if l.Chain.TargetHex != "" {
			l.Chain.Target = block.TargetFromBits(l.Chain.TargetHex)
		} else {
			l.Chain.Target = block.InitialTarget
		}
	}

	log.Printf("[LEDGER] Loaded %d blocks from %s", l.Chain.Len(), filePath)
	return &l
}

// ──────────────────────────────────────────────────────────────────────────────
// Accessors
// ──────────────────────────────────────────────────────────────────────────────

// GetChain returns a pointer to the underlying blockchain.
func (l *Ledger) GetChain() *chain.Blockchain {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.Chain
}

// GetChainHeight returns the current blockchain height.
func (l *Ledger) GetChainHeight() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.Chain.Height()
}

// GetBlockReward returns the mining reward for the next block.
func (l *Ledger) GetBlockReward() float64 {
	return l.Chain.GetBlockReward()
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// shortAddr returns the first 8 and last 4 chars of an address for logging.
func shortAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-4:]
}
