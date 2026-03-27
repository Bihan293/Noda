// Package ledger manages the blockchain, UTXO set, mempool, and validates transactions.
//
// It combines:
//   - Blockchain (ordered sequence of blocks)
//   - UTXO set (unspent transaction outputs for balance tracking)
//   - Mempool (pool of unconfirmed transactions)
//   - Faucet: 5,000 coins per request, global cap 11,000,000 total (no per-address cooldown)
//   - Mining rewards with halving
//
// The ledger uses UTXO for balance computation instead of a flat account map.
// Per-address faucet cooldown is removed; the only limit is the global 11M cap.
//
// Genesis ownership:
//
//	The genesis supply (11M) is assigned to an address derived from the configured
//	faucet/genesis private key. The genesis owner is stored in chain metadata and
//	verified on every restart. A mismatched key causes a fail-fast error.
package ledger

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Bihan293/Noda/block"
	"github.com/Bihan293/Noda/chain"
	"github.com/Bihan293/Noda/crypto"
	"github.com/Bihan293/Noda/mempool"
	"github.com/Bihan293/Noda/utxo"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	// StorageFile is the default file used to persist chain and balances.
	StorageFile = "node_data.json"

	// FaucetAmount is how many coins the faucet distributes per request.
	FaucetAmount = 5000.0

	// FaucetGlobalCap is the maximum total coins that can be distributed via faucet.
	// Once 11,000,000 coins have been distributed, the faucet is permanently disabled.
	FaucetGlobalCap = 11_000_000.0
)

// ──────────────────────────────────────────────────────────────────────────────
// Errors
// ──────────────────────────────────────────────────────────────────────────────

// ErrGenesisOwnerMismatch is returned when the provided faucet key does not
// match the genesis owner recorded in the chain.
var ErrGenesisOwnerMismatch = fmt.Errorf("genesis owner mismatch")

// ──────────────────────────────────────────────────────────────────────────────
// Ledger
// ──────────────────────────────────────────────────────────────────────────────

// Ledger holds the full blockchain, UTXO set, mempool, and faucet state.
type Ledger struct {
	Chain    *chain.Blockchain  `json:"chain"`
	Balances map[string]float64 `json:"balances"` // kept for JSON compat; rebuilt from UTXO
	mu       sync.RWMutex
	filePath string

	// UTXO set — the source of truth for balances.
	UTXOSet *utxo.Set `json:"-"` // rebuilt from chain, not persisted as JSON field

	// Mempool — unconfirmed transaction pool.
	Mempool *mempool.Mempool `json:"-"` // transient, not persisted

	// Faucet state
	faucetPrivKey string // hex-encoded Ed25519 private key for faucet wallet
	faucetAddress string // derived from faucetPrivKey
}

// NewLedger creates a new ledger with a genesis blockchain using the legacy
// hardcoded genesis address. For new networks, use NewLedgerWithOwner.
func NewLedger(filePath string) *Ledger {
	return NewLedgerWithOwner(filePath, block.LegacyGenesisAddress)
}

// NewLedgerWithOwner creates a new ledger with a genesis blockchain where the
// genesis supply (11M) belongs to the specified owner address.
func NewLedgerWithOwner(filePath string, genesisOwner string) *Ledger {
	bc := chain.NewBlockchainWithOwner(genesisOwner)

	// Build UTXO set from genesis block.
	utxoSet, err := utxo.RebuildFromBlocks(bc.Blocks)
	if err != nil {
		slog.Warn("UTXO rebuild failed, starting with empty set", "error", err)
		utxoSet = utxo.NewSet()
	}

	// Derive balances from UTXO.
	balances := utxoSet.AllBalances()

	l := &Ledger{
		Chain:    bc,
		Balances: balances,
		filePath: filePath,
		UTXOSet:  utxoSet,
		Mempool:  mempool.New(mempool.DefaultMaxSize),
	}

	slog.Info("New ledger created",
		"genesis_supply", block.GenesisSupply,
		"genesis_owner", genesisOwner,
		"utxo_count", utxoSet.Size(),
	)
	return l
}

// ──────────────────────────────────────────────────────────────────────────────
// Genesis Owner Management
// ──────────────────────────────────────────────────────────────────────────────

// GenesisOwner returns the address that owns the genesis supply, as recorded
// in the chain metadata or extracted from the genesis block.
func (l *Ledger) GenesisOwner() string {
	// Prefer the explicit metadata field.
	if l.Chain.GenesisOwner != "" {
		return l.Chain.GenesisOwner
	}
	// Fall back to inspecting the genesis block itself.
	if len(l.Chain.Blocks) > 0 {
		if owner, ok := block.GenesisOwnerFromBlock(l.Chain.Blocks[0]); ok {
			return owner
		}
	}
	return ""
}

// FaucetOwnerMatch returns true if the configured faucet address matches the
// genesis owner (i.e. the faucet actually controls the genesis funds).
func (l *Ledger) FaucetOwnerMatch() bool {
	if l.faucetAddress == "" {
		return false
	}
	return l.faucetAddress == l.GenesisOwner()
}

// UsableFaucetBalance returns the actual spendable balance on the genesis/faucet
// address from the UTXO set. If faucet address != genesis owner, returns 0.
func (l *Ledger) UsableFaucetBalance() float64 {
	if !l.FaucetOwnerMatch() {
		return 0
	}
	return l.UTXOSet.Balance(l.faucetAddress)
}

// SetFaucetKeyAndValidateGenesis configures the faucet wallet and ensures the
// key matches the genesis owner recorded in the chain.
//
// On a NEW chain (height 0, no faucet distributed):
//   - If the chain still uses the legacy hardcoded genesis address, perform a
//     one-time migration to rebind genesis to the provided key.
//
// On an EXISTING chain:
//   - If the key does not match the recorded genesis owner, return a fail-fast
//     error with a clear message.
func (l *Ledger) SetFaucetKeyAndValidateGenesis(privKeyHex string) error {
	addr, err := crypto.AddressFromPrivateKey(privKeyHex)
	if err != nil {
		return fmt.Errorf("invalid faucet private key: %w", err)
	}

	currentGenesisOwner := l.GenesisOwner()

	// ── Case 1: Key matches the existing genesis owner — happy path ──
	if addr == currentGenesisOwner {
		l.faucetPrivKey = privKeyHex
		l.faucetAddress = addr
		slog.Info("Faucet wallet configured (matches genesis owner)", "address", addr)
		return nil
	}

	// ── Case 2: Fresh chain or legacy chain at height 0 — safe migration ──
	if l.canMigrateLegacyGenesis() {
		slog.Info("Migrating genesis owner from legacy address to configured key",
			"old_owner", currentGenesisOwner,
			"new_owner", addr,
		)
		if err := l.migrateLegacyGenesis(addr); err != nil {
			return fmt.Errorf("legacy genesis migration failed: %w", err)
		}
		l.faucetPrivKey = privKeyHex
		l.faucetAddress = addr
		slog.Info("Faucet wallet configured after genesis migration", "address", addr)
		return nil
	}

	// ── Case 3: Existing chain with incompatible key — fail fast ──
	return fmt.Errorf("%w: the chain records genesis owner as %s, "+
		"but the provided faucet key derives address %s. "+
		"Either use the correct FAUCET_KEY for this chain, or start a new chain",
		ErrGenesisOwnerMismatch, shortAddr(currentGenesisOwner), shortAddr(addr))
}

// canMigrateLegacyGenesis returns true if the chain is eligible for a one-time
// migration of the genesis owner. Conditions:
//   - Chain height is 0 (only genesis block)
//   - No faucet coins have been distributed yet
//   - Current genesis owner is the legacy hardcoded address
func (l *Ledger) canMigrateLegacyGenesis() bool {
	if l.Chain.Height() > 0 {
		return false
	}
	if l.Chain.TotalFaucet > 0 {
		return false
	}
	currentOwner := l.GenesisOwner()
	return currentOwner == block.LegacyGenesisAddress
}

// migrateLegacyGenesis replaces the legacy genesis block with one that assigns
// the supply to the new owner address, and rebuilds the UTXO set.
func (l *Ledger) migrateLegacyGenesis(newOwnerAddress string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Replace the chain with a fresh one rooted at the new owner.
	bc := chain.NewBlockchainWithOwner(newOwnerAddress)

	// Rebuild UTXO set.
	utxoSet, err := utxo.RebuildFromBlocks(bc.Blocks)
	if err != nil {
		return fmt.Errorf("UTXO rebuild after genesis migration failed: %w", err)
	}

	l.Chain = bc
	l.UTXOSet = utxoSet
	l.Balances = utxoSet.AllBalances()

	// Persist immediately.
	if err := l.saveLocked(); err != nil {
		slog.Warn("Failed to persist after genesis migration", "error", err)
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Faucet Configuration (legacy entry point)
// ──────────────────────────────────────────────────────────────────────────────

// SetFaucetKey configures the faucet wallet from a hex-encoded private key.
// This is a simplified version that does NOT validate genesis ownership.
// Prefer SetFaucetKeyAndValidateGenesis for production startup.
func (l *Ledger) SetFaucetKey(privKeyHex string) error {
	addr, err := crypto.AddressFromPrivateKey(privKeyHex)
	if err != nil {
		return fmt.Errorf("invalid faucet private key: %w", err)
	}
	l.faucetPrivKey = privKeyHex
	l.faucetAddress = addr
	slog.Info("Faucet wallet configured", "address", addr)
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
// Now also requires that the faucet address matches the genesis owner.
func (l *Ledger) IsFaucetActive() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.faucetPrivKey != "" &&
		l.faucetAddress == l.Chain.GenesisOwner &&
		l.Chain.TotalFaucet < FaucetGlobalCap
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
// Balance Queries (from UTXO set)
// ──────────────────────────────────────────────────────────────────────────────

// GetBalance returns the balance for a given address from the UTXO set.
func (l *Ledger) GetBalance(address string) float64 {
	return l.UTXOSet.Balance(address)
}

// GetAllBalances returns a copy of all balances derived from the UTXO set.
func (l *Ledger) GetAllBalances() map[string]float64 {
	return l.UTXOSet.AllBalances()
}

// ──────────────────────────────────────────────────────────────────────────────
// Transaction Validation
// ──────────────────────────────────────────────────────────────────────────────

// ValidateUserTx checks a user-submitted transaction without applying it.
// Validates: amount, addresses, signature, UTXO balance, mempool double-spend.
func (l *Ledger) ValidateUserTx(tx block.Transaction) error {
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

	// Balance check using UTXO set.
	utxoBalance := l.UTXOSet.Balance(tx.From)
	// Account for pending spends in the mempool.
	pendingSpend := l.Mempool.GetSpendingTotal(tx.From)
	availableBalance := utxoBalance - pendingSpend

	if availableBalance < tx.Amount {
		return fmt.Errorf("insufficient balance: address %s has %.6f available (%.6f UTXO - %.6f pending), tried to send %.6f",
			shortAddr(tx.From), availableBalance, utxoBalance, pendingSpend, tx.Amount)
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Transaction Processing
// ──────────────────────────────────────────────────────────────────────────────

// SubmitTransaction validates a user transaction and adds it to the mempool.
// Then creates a block containing the transaction (single-tx block for now).
func (l *Ledger) SubmitTransaction(tx block.Transaction) error {
	// Validate the transaction.
	if err := l.ValidateUserTx(tx); err != nil {
		return err
	}

	// Set timestamp and compute ID.
	tx.Timestamp = time.Now().Unix()
	tx.ID = block.HashTransaction(tx)

	// Add to mempool.
	if err := l.Mempool.Add(tx); err != nil {
		return fmt.Errorf("mempool rejection: %w", err)
	}

	// Mine a block containing this transaction.
	if err := l.mineBlockWithTx(tx); err != nil {
		// Remove from mempool on failure.
		l.Mempool.Remove(tx.ID)
		return err
	}

	return nil
}

// ValidateAndProcessUserTx is the legacy entry point — wraps SubmitTransaction.
func (l *Ledger) ValidateAndProcessUserTx(tx block.Transaction) error {
	return l.SubmitTransaction(tx)
}

// mineBlockWithTx creates and mines a block containing a single transaction.
func (l *Ledger) mineBlockWithTx(tx block.Transaction) error {
	l.mu.Lock()
	defer l.mu.Unlock()

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

	// Mine the block.
	if err := block.MineBlock(newBlock, target, 10_000_000); err != nil {
		return fmt.Errorf("mining failed for user tx block: %w", err)
	}

	// Add block to chain.
	if err := l.Chain.AddBlock(newBlock); err != nil {
		return fmt.Errorf("failed to add block: %w", err)
	}

	// Apply block to UTXO set.
	if err := l.UTXOSet.ApplyBlock(newBlock); err != nil {
		return fmt.Errorf("UTXO update failed: %w", err)
	}

	// Remove transaction from mempool.
	l.Mempool.Remove(tx.ID)

	// Update balance cache from UTXO.
	l.Balances = l.UTXOSet.AllBalances()

	slog.Info("TX accepted",
		"from", shortAddr(tx.From),
		"to", shortAddr(tx.To),
		"amount", tx.Amount,
		"block_height", newBlock.Header.Height,
		"utxo_size", l.UTXOSet.Size(),
	)

	// Persist.
	_ = l.saveLocked()
	return nil
}

// ProcessTransaction validates a single transaction and applies it to balances.
// Used for processing transactions within a received block (not user-submitted).
func (l *Ledger) ProcessTransaction(tx block.Transaction) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if tx.Amount <= 0 {
		return fmt.Errorf("invalid amount: must be positive, got %f", tx.Amount)
	}

	// Coinbase: just credit.
	if tx.From == "" {
		l.Balances[tx.To] += tx.Amount
		return nil
	}

	if tx.To == "" {
		return fmt.Errorf("invalid addresses: 'to' is required")
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

	l.Balances[tx.From] -= tx.Amount
	l.Balances[tx.To] += tx.Amount
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Faucet — Global cap 11M, 5000 coins/request, no per-address cooldown
// ──────────────────────────────────────────────────────────────────────────────

// ProcessFaucet sends FaucetAmount coins from the faucet wallet to the given address.
// Enforces only the global faucet cap (11M total). No per-address cooldown.
// Multiple claims allowed from any address until the global cap is reached.
func (l *Ledger) ProcessFaucet(toAddress string) (*block.Transaction, error) {
	// Check faucet is configured.
	if l.faucetPrivKey == "" || l.faucetAddress == "" {
		return nil, fmt.Errorf("faucet not configured: start node with -faucet-key flag")
	}

	// Verify faucet address controls genesis funds.
	if !l.FaucetOwnerMatch() {
		return nil, fmt.Errorf("faucet address %s does not match genesis owner %s — faucet cannot spend genesis funds",
			shortAddr(l.faucetAddress), shortAddr(l.GenesisOwner()))
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
		return nil, fmt.Errorf("faucet exhausted: all %.0f coins have been distributed — faucet is permanently disabled", FaucetGlobalCap)
	}

	// Calculate actual amount (may be less than FaucetAmount near the cap).
	amount := FaucetAmount
	remaining := FaucetGlobalCap - totalDistributed
	if amount > remaining {
		amount = remaining
	}

	// Check faucet wallet has enough balance.
	faucetBalance := l.UTXOSet.Balance(l.faucetAddress)
	if faucetBalance < amount {
		return nil, fmt.Errorf("faucet wallet insufficient balance: has %.2f, needs %.2f", faucetBalance, amount)
	}

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
	if err := l.SubmitTransaction(tx); err != nil {
		return nil, fmt.Errorf("faucet transaction failed: %w", err)
	}

	// Update faucet tracking.
	l.mu.Lock()
	l.Chain.TotalFaucet += amount
	l.mu.Unlock()

	slog.Info("Faucet distribution",
		"to", shortAddr(toAddress),
		"amount", amount,
		"total_distributed", totalDistributed+amount,
		"global_cap", FaucetGlobalCap,
	)

	return &tx, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Chain Sync
// ──────────────────────────────────────────────────────────────────────────────

// ReplaceChain replaces the current chain if the new one is longer and valid.
// Rebuilds the UTXO set from the new chain.
func (l *Ledger) ReplaceChain(newChain *chain.Blockchain) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if newChain.Len() <= l.Chain.Len() {
		return false
	}

	// Validate the full chain.
	if err := chain.ValidateChain(newChain); err != nil {
		slog.Warn("Sync rejected: invalid chain", "error", err)
		return false
	}

	// Rebuild UTXO set from the new chain.
	newUTXO, err := utxo.RebuildFromBlocks(newChain.Blocks)
	if err != nil {
		slog.Warn("Sync rejected: UTXO rebuild failed", "error", err)
		return false
	}

	// Rebuild balances from UTXO.
	balances := newUTXO.AllBalances()

	l.Chain = newChain
	l.UTXOSet = newUTXO
	l.Balances = balances
	_ = l.saveLocked()

	slog.Info("Chain replaced",
		"blocks", newChain.Len(),
		"utxo_count", newUTXO.Size(),
	)
	return true
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
		slog.Info("No data file found, starting fresh", "path", filePath)
		return NewLedger(filePath)
	}

	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		slog.Warn("Failed to parse data file, starting fresh", "path", filePath, "error", err)
		return NewLedger(filePath)
	}
	l.filePath = filePath
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

	// Back-fill GenesisOwner from genesis block if metadata is missing (legacy data).
	if l.Chain != nil && l.Chain.GenesisOwner == "" && len(l.Chain.Blocks) > 0 {
		if owner, ok := block.GenesisOwnerFromBlock(l.Chain.Blocks[0]); ok {
			l.Chain.GenesisOwner = owner
			slog.Info("Back-filled genesis owner from genesis block", "genesis_owner", owner)
		}
	}

	// Rebuild UTXO set from the loaded chain.
	if l.Chain != nil {
		utxoSet, err := utxo.RebuildFromBlocks(l.Chain.Blocks)
		if err != nil {
			slog.Warn("UTXO rebuild failed, using balance map fallback", "error", err)
			utxoSet = utxo.NewSet()
		} else {
			// Sync balances from UTXO set.
			l.Balances = utxoSet.AllBalances()
		}
		l.UTXOSet = utxoSet
	} else {
		l.UTXOSet = utxo.NewSet()
	}

	// Initialize mempool (transient, not persisted).
	l.Mempool = mempool.New(mempool.DefaultMaxSize)

	slog.Info("Ledger loaded",
		"blocks", l.Chain.Len(),
		"path", filePath,
		"utxo_count", l.UTXOSet.Size(),
		"genesis_owner", l.GenesisOwner(),
	)
	return &l
}

// LoadLedgerWithOwner reads a ledger from a JSON file with a specific genesis owner.
// If no file exists, creates a new chain with the given owner.
func LoadLedgerWithOwner(filePath string, genesisOwner string) *Ledger {
	data, err := os.ReadFile(filePath)
	if err != nil {
		slog.Info("No data file found, starting fresh with configured genesis owner",
			"path", filePath,
			"genesis_owner", genesisOwner,
		)
		return NewLedgerWithOwner(filePath, genesisOwner)
	}

	// File exists — load normally (genesis owner validated later via SetFaucetKeyAndValidateGenesis).
	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		slog.Warn("Failed to parse data file, starting fresh", "path", filePath, "error", err)
		return NewLedgerWithOwner(filePath, genesisOwner)
	}
	l.filePath = filePath
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

	// Back-fill GenesisOwner from genesis block if metadata is missing (legacy data).
	if l.Chain != nil && l.Chain.GenesisOwner == "" && len(l.Chain.Blocks) > 0 {
		if owner, ok := block.GenesisOwnerFromBlock(l.Chain.Blocks[0]); ok {
			l.Chain.GenesisOwner = owner
			slog.Info("Back-filled genesis owner from genesis block", "genesis_owner", owner)
		}
	}

	// Rebuild UTXO set from the loaded chain.
	if l.Chain != nil {
		utxoSet, err := utxo.RebuildFromBlocks(l.Chain.Blocks)
		if err != nil {
			slog.Warn("UTXO rebuild failed, using balance map fallback", "error", err)
			utxoSet = utxo.NewSet()
		} else {
			l.Balances = utxoSet.AllBalances()
		}
		l.UTXOSet = utxoSet
	} else {
		l.UTXOSet = utxo.NewSet()
	}

	l.Mempool = mempool.New(mempool.DefaultMaxSize)

	slog.Info("Ledger loaded",
		"blocks", l.Chain.Len(),
		"path", filePath,
		"utxo_count", l.UTXOSet.Size(),
		"genesis_owner", l.GenesisOwner(),
	)
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

// GetMempoolSize returns the number of transactions in the mempool.
func (l *Ledger) GetMempoolSize() int {
	return l.Mempool.Size()
}

// GetPendingTransactions returns up to limit pending transactions from the mempool.
func (l *Ledger) GetPendingTransactions(limit int) []block.Transaction {
	return l.Mempool.GetPending(limit)
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
