package ledger

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bihan293/Noda/block"
	"github.com/Bihan293/Noda/crypto"
)

func tmpFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test_ledger.json")
}

func TestNewLedger(t *testing.T) {
	l := NewLedger(tmpFile(t))
	if l == nil {
		t.Fatal("NewLedger() returned nil")
	}
	if l.Chain == nil {
		t.Error("Chain is nil")
	}
	if l.UTXOSet == nil {
		t.Error("UTXOSet is nil")
	}
	if l.Mempool == nil {
		t.Error("Mempool is nil")
	}
	if l.GetChainHeight() != 0 {
		t.Errorf("GetChainHeight() = %d, want 0", l.GetChainHeight())
	}
}

func TestGetBalance_Genesis(t *testing.T) {
	l := NewLedger(tmpFile(t))
	balance := l.GetBalance(block.LegacyGenesisAddress)
	if balance != block.GenesisSupply {
		t.Errorf("genesis balance = %f, want %f", balance, block.GenesisSupply)
	}
}

func TestGetBalance_Unknown(t *testing.T) {
	l := NewLedger(tmpFile(t))
	balance := l.GetBalance("unknown_address")
	if balance != 0 {
		t.Errorf("unknown balance = %f, want 0", balance)
	}
}

func TestGetAllBalances(t *testing.T) {
	l := NewLedger(tmpFile(t))
	balances := l.GetAllBalances()
	if balances[block.LegacyGenesisAddress] != block.GenesisSupply {
		t.Errorf("genesis balance = %f, want %f", balances[block.LegacyGenesisAddress], block.GenesisSupply)
	}
}

func TestGetBlockReward(t *testing.T) {
	l := NewLedger(tmpFile(t))
	reward := l.GetBlockReward()
	if reward != 50.0 {
		t.Errorf("GetBlockReward() = %f, want 50", reward)
	}
}

func TestGetMempoolSize(t *testing.T) {
	l := NewLedger(tmpFile(t))
	if l.GetMempoolSize() != 0 {
		t.Errorf("GetMempoolSize() = %d, want 0", l.GetMempoolSize())
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Faucet
// ──────────────────────────────────────────────────────────────────────────────

func TestSetFaucetKey(t *testing.T) {
	l := NewLedger(tmpFile(t))
	kp, _ := crypto.GenerateKeyPair()
	privHex := hex.EncodeToString(kp.PrivateKey)

	err := l.SetFaucetKey(privHex)
	if err != nil {
		t.Fatalf("SetFaucetKey() error: %v", err)
	}
	if l.FaucetAddress() != kp.Address {
		t.Errorf("FaucetAddress() = %s, want %s", l.FaucetAddress(), kp.Address)
	}
}

func TestSetFaucetKey_Invalid(t *testing.T) {
	l := NewLedger(tmpFile(t))
	err := l.SetFaucetKey("invalid")
	if err == nil {
		t.Error("SetFaucetKey() should fail for invalid key")
	}
}

func TestFaucetState_NotConfigured(t *testing.T) {
	l := NewLedger(tmpFile(t))

	if l.FaucetAddress() != "" {
		t.Error("FaucetAddress() should be empty when not configured")
	}
	if l.IsFaucetActive() {
		t.Error("IsFaucetActive() should be false when not configured")
	}
}

func TestFaucetTotalDistributed(t *testing.T) {
	l := NewLedger(tmpFile(t))
	if l.FaucetTotalDistributed() != 0 {
		t.Errorf("FaucetTotalDistributed() = %f, want 0", l.FaucetTotalDistributed())
	}
}

func TestFaucetRemaining(t *testing.T) {
	l := NewLedger(tmpFile(t))
	remaining := l.FaucetRemaining()
	if remaining != FaucetGlobalCap {
		t.Errorf("FaucetRemaining() = %f, want %f", remaining, FaucetGlobalCap)
	}
}

func TestProcessFaucet_NotConfigured(t *testing.T) {
	l := NewLedger(tmpFile(t))
	_, err := l.ProcessFaucet("some_address")
	if err == nil {
		t.Error("ProcessFaucet() should fail when faucet not configured")
	}
}

func TestProcessFaucet_EmptyAddress(t *testing.T) {
	l := NewLedger(tmpFile(t))
	kp, _ := crypto.GenerateKeyPair()
	l.SetFaucetKey(hex.EncodeToString(kp.PrivateKey))

	_, err := l.ProcessFaucet("")
	if err == nil {
		t.Error("ProcessFaucet() should fail for empty address")
	}
}

func TestProcessFaucet_SelfSend(t *testing.T) {
	l := NewLedger(tmpFile(t))
	kp, _ := crypto.GenerateKeyPair()
	l.SetFaucetKey(hex.EncodeToString(kp.PrivateKey))

	_, err := l.ProcessFaucet(kp.Address)
	if err == nil {
		t.Error("ProcessFaucet() should fail when sending to faucet itself")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Transaction Validation
// ──────────────────────────────────────────────────────────────────────────────

func TestValidateUserTx_NegativeAmount(t *testing.T) {
	l := NewLedger(tmpFile(t))
	tx := block.Transaction{Amount: -5, From: "a", To: "b"}
	err := l.ValidateUserTx(tx)
	if err == nil {
		t.Error("ValidateUserTx() should fail for negative amount")
	}
}

func TestValidateUserTx_ZeroAmount(t *testing.T) {
	l := NewLedger(tmpFile(t))
	tx := block.Transaction{Amount: 0, From: "a", To: "b"}
	err := l.ValidateUserTx(tx)
	if err == nil {
		t.Error("ValidateUserTx() should fail for zero amount")
	}
}

func TestValidateUserTx_NoFrom(t *testing.T) {
	l := NewLedger(tmpFile(t))
	tx := block.Transaction{Amount: 10, From: "", To: "b"}
	err := l.ValidateUserTx(tx)
	if err == nil {
		t.Error("ValidateUserTx() should fail for empty 'from'")
	}
}

func TestValidateUserTx_NoTo(t *testing.T) {
	l := NewLedger(tmpFile(t))
	tx := block.Transaction{Amount: 10, From: "a", To: ""}
	err := l.ValidateUserTx(tx)
	if err == nil {
		t.Error("ValidateUserTx() should fail for empty 'to'")
	}
}

func TestValidateUserTx_SelfSend(t *testing.T) {
	l := NewLedger(tmpFile(t))
	tx := block.Transaction{Amount: 10, From: "a", To: "a", Signature: "sig"}
	err := l.ValidateUserTx(tx)
	if err == nil {
		t.Error("ValidateUserTx() should fail for self-send")
	}
}

func TestValidateUserTx_InvalidSignature(t *testing.T) {
	l := NewLedger(tmpFile(t))
	tx := block.Transaction{Amount: 10, From: "aabbccdd", To: "eeffaabb", Signature: "bad_sig"}
	err := l.ValidateUserTx(tx)
	if err == nil {
		t.Error("ValidateUserTx() should fail for invalid signature")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Persistence
// ──────────────────────────────────────────────────────────────────────────────

func TestSaveAndLoad(t *testing.T) {
	path := tmpFile(t)

	l := NewLedger(path)
	if err := l.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// File should exist.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Save() did not create file")
	}

	// Load should succeed.
	l2 := LoadLedger(path)
	if l2 == nil {
		t.Fatal("LoadLedger() returned nil")
	}
	if l2.GetChainHeight() != l.GetChainHeight() {
		t.Errorf("loaded height = %d, want %d", l2.GetChainHeight(), l.GetChainHeight())
	}
	if l2.UTXOSet == nil {
		t.Error("loaded UTXOSet is nil")
	}
	if l2.Mempool == nil {
		t.Error("loaded Mempool is nil")
	}
}

func TestLoadLedger_FileNotFound(t *testing.T) {
	l := LoadLedger("/tmp/nonexistent_ledger_test.json")
	if l == nil {
		t.Fatal("LoadLedger() returned nil for missing file")
	}
	// Should create a fresh ledger.
	if l.GetChainHeight() != 0 {
		t.Errorf("height = %d, want 0", l.GetChainHeight())
	}
}

func TestLoadLedger_InvalidJSON(t *testing.T) {
	path := tmpFile(t)
	os.WriteFile(path, []byte("not valid json"), 0644)

	l := LoadLedger(path)
	if l == nil {
		t.Fatal("LoadLedger() returned nil for invalid JSON")
	}
	// Should fallback to a fresh ledger.
	if l.GetChainHeight() != 0 {
		t.Errorf("height = %d, want 0", l.GetChainHeight())
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Chain Replacement
// ──────────────────────────────────────────────────────────────────────────────

func TestReplaceChain_Shorter(t *testing.T) {
	l := NewLedger(tmpFile(t))

	// Build a "shorter" chain (same length as genesis).
	// ReplaceChain should reject it.
	shorter := l.GetChain()
	replaced := l.ReplaceChain(shorter)
	if replaced {
		t.Error("ReplaceChain() should not accept a same-length chain")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Accessors
// ──────────────────────────────────────────────────────────────────────────────

func TestGetChain(t *testing.T) {
	l := NewLedger(tmpFile(t))
	c := l.GetChain()
	if c == nil {
		t.Error("GetChain() returned nil")
	}
}

func TestGetPendingTransactions(t *testing.T) {
	l := NewLedger(tmpFile(t))
	pending := l.GetPendingTransactions(10)
	if len(pending) != 0 {
		t.Errorf("GetPendingTransactions() len = %d, want 0", len(pending))
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// [CRITICAL-1] Genesis/Faucet Ownership Tests
// ──────────────────────────────────────────────────────────────────────────────

// Test that a new random key gets control over genesis/faucet funds.
func TestNewKeyGetsGenesisControl(t *testing.T) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privHex := hex.EncodeToString(kp.PrivateKey)

	// Create a fresh ledger with the key's address as genesis owner.
	l := NewLedgerWithOwner(tmpFile(t), kp.Address)

	// Genesis owner should be the new key's address.
	if l.GenesisOwner() != kp.Address {
		t.Errorf("GenesisOwner() = %s, want %s", l.GenesisOwner(), kp.Address)
	}

	// Balance should be on the new key's address.
	balance := l.GetBalance(kp.Address)
	if balance != block.GenesisSupply {
		t.Errorf("genesis balance = %f, want %f", balance, block.GenesisSupply)
	}

	// SetFaucetKeyAndValidateGenesis should succeed for matching key.
	if err := l.SetFaucetKeyAndValidateGenesis(privHex); err != nil {
		t.Fatalf("SetFaucetKeyAndValidateGenesis() error: %v", err)
	}

	// Faucet should match genesis owner.
	if !l.FaucetOwnerMatch() {
		t.Error("FaucetOwnerMatch() should be true")
	}

	// Usable faucet balance should be the genesis supply.
	usable := l.UsableFaucetBalance()
	if usable != block.GenesisSupply {
		t.Errorf("UsableFaucetBalance() = %f, want %f", usable, block.GenesisSupply)
	}

	// Faucet should be active.
	if !l.IsFaucetActive() {
		t.Error("IsFaucetActive() should be true")
	}
}

// Test that a faucet can actually distribute coins when key matches genesis.
func TestFaucetWorksWithMatchingKey(t *testing.T) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privHex := hex.EncodeToString(kp.PrivateKey)

	recipient, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	l := NewLedgerWithOwner(tmpFile(t), kp.Address)
	if err := l.SetFaucetKeyAndValidateGenesis(privHex); err != nil {
		t.Fatalf("SetFaucetKeyAndValidateGenesis() error: %v", err)
	}

	// ProcessFaucet should succeed.
	tx, err := l.ProcessFaucet(recipient.Address)
	if err != nil {
		t.Fatalf("ProcessFaucet() error: %v", err)
	}
	if tx == nil {
		t.Fatal("ProcessFaucet() returned nil tx")
	}
	if tx.Amount != FaucetAmount {
		t.Errorf("faucet tx amount = %f, want %f", tx.Amount, FaucetAmount)
	}

	// Recipient should have received coins.
	recipientBalance := l.GetBalance(recipient.Address)
	if recipientBalance != FaucetAmount {
		t.Errorf("recipient balance = %f, want %f", recipientBalance, FaucetAmount)
	}
}

// Test that an incompatible key on an existing chain causes a clear error.
func TestIncompatibleKeyFailsFast(t *testing.T) {
	kp1, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	kp2, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	priv1Hex := hex.EncodeToString(kp1.PrivateKey)
	priv2Hex := hex.EncodeToString(kp2.PrivateKey)

	path := tmpFile(t)

	// Create chain with kp1 as genesis owner.
	l := NewLedgerWithOwner(path, kp1.Address)
	if err := l.SetFaucetKeyAndValidateGenesis(priv1Hex); err != nil {
		t.Fatalf("first SetFaucetKeyAndValidateGenesis() error: %v", err)
	}

	// Do a faucet transaction to move chain beyond genesis-only state.
	recipient, _ := crypto.GenerateKeyPair()
	_, err = l.ProcessFaucet(recipient.Address)
	if err != nil {
		t.Fatalf("ProcessFaucet() error: %v", err)
	}

	// Save and reload.
	if err := l.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	l2 := LoadLedger(path)

	// Try using kp2 (different key) — should fail fast.
	err = l2.SetFaucetKeyAndValidateGenesis(priv2Hex)
	if err == nil {
		t.Fatal("SetFaucetKeyAndValidateGenesis() should fail for incompatible key")
	}
	if !isGenesisOwnerMismatch(err) {
		t.Errorf("expected genesis owner mismatch error, got: %v", err)
	}
}

// Test that legacy height=0 chain migrates safely.
func TestLegacyGenesisMigration(t *testing.T) {
	path := tmpFile(t)

	// Create a legacy-style ledger (uses hardcoded LegacyGenesisAddress).
	l := NewLedger(path)

	// Verify it uses the legacy address.
	if l.GenesisOwner() != block.LegacyGenesisAddress {
		t.Fatalf("expected legacy genesis owner %s, got %s",
			block.LegacyGenesisAddress, l.GenesisOwner())
	}

	// Balance should be on legacy address.
	legacyBalance := l.GetBalance(block.LegacyGenesisAddress)
	if legacyBalance != block.GenesisSupply {
		t.Fatalf("legacy genesis balance = %f, want %f", legacyBalance, block.GenesisSupply)
	}

	// Generate a new key and attempt migration.
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privHex := hex.EncodeToString(kp.PrivateKey)

	// Migration should succeed since height=0 and no faucet distributed.
	if err := l.SetFaucetKeyAndValidateGenesis(privHex); err != nil {
		t.Fatalf("SetFaucetKeyAndValidateGenesis() for legacy migration error: %v", err)
	}

	// Genesis owner should now be the new key.
	if l.GenesisOwner() != kp.Address {
		t.Errorf("after migration GenesisOwner() = %s, want %s", l.GenesisOwner(), kp.Address)
	}

	// Balance should have moved to the new address.
	newBalance := l.GetBalance(kp.Address)
	if newBalance != block.GenesisSupply {
		t.Errorf("after migration balance = %f, want %f", newBalance, block.GenesisSupply)
	}

	// Legacy address should have zero balance.
	legacyBalance = l.GetBalance(block.LegacyGenesisAddress)
	if legacyBalance != 0 {
		t.Errorf("legacy address still has balance %f after migration", legacyBalance)
	}
}

// Test that legacy chain beyond height=0 does NOT allow migration.
func TestLegacyMigration_BlockedAfterActivity(t *testing.T) {
	path := tmpFile(t)

	// Create a legacy ledger — faucet uses a key that does NOT match legacy address.
	l := NewLedger(path)

	// Simulate some faucet activity by incrementing TotalFaucet.
	l.Chain.TotalFaucet = 5000

	// Generate a new key.
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privHex := hex.EncodeToString(kp.PrivateKey)

	// Migration should be blocked because TotalFaucet > 0.
	err = l.SetFaucetKeyAndValidateGenesis(privHex)
	if err == nil {
		t.Fatal("SetFaucetKeyAndValidateGenesis() should fail — migration blocked after faucet activity")
	}
}

// Test GenesisOwner is persisted and restored correctly.
func TestGenesisOwnerPersistence(t *testing.T) {
	path := tmpFile(t)

	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	// Create ledger with specific genesis owner.
	l := NewLedgerWithOwner(path, kp.Address)
	if err := l.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Reload and verify.
	l2 := LoadLedger(path)
	if l2.GenesisOwner() != kp.Address {
		t.Errorf("loaded GenesisOwner() = %s, want %s", l2.GenesisOwner(), kp.Address)
	}
}

// Test that /status endpoint returns genesis_owner info.
func TestStatusShowsGenesisOwner(t *testing.T) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	l := NewLedgerWithOwner(tmpFile(t), kp.Address)

	owner := l.GenesisOwner()
	if owner != kp.Address {
		t.Errorf("GenesisOwner() = %s, want %s", owner, kp.Address)
	}

	// Without faucet configured, FaucetOwnerMatch should be false.
	if l.FaucetOwnerMatch() {
		t.Error("FaucetOwnerMatch() should be false without faucet configured")
	}

	// UsableFaucetBalance should be 0 when no match.
	if l.UsableFaucetBalance() != 0 {
		t.Errorf("UsableFaucetBalance() = %f, want 0", l.UsableFaucetBalance())
	}
}

// Test LoadLedgerWithOwner creates a new chain with the correct owner.
func TestLoadLedgerWithOwner_NewFile(t *testing.T) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	path := tmpFile(t)
	l := LoadLedgerWithOwner(path, kp.Address)

	if l.GenesisOwner() != kp.Address {
		t.Errorf("GenesisOwner() = %s, want %s", l.GenesisOwner(), kp.Address)
	}
	if l.GetBalance(kp.Address) != block.GenesisSupply {
		t.Errorf("balance = %f, want %f", l.GetBalance(kp.Address), block.GenesisSupply)
	}
}

// isGenesisOwnerMismatch checks if the error is a genesis owner mismatch.
func isGenesisOwnerMismatch(err error) bool {
	if err == nil {
		return false
	}
	// Check using errors.Is-like behavior.
	return err.Error() != "" && (err == ErrGenesisOwnerMismatch ||
		len(err.Error()) > len("genesis owner mismatch") &&
			err.Error()[:len("genesis owner mismatch")] == "genesis owner mismatch")
}
