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
	balance := l.GetBalance(block.GenesisAddress)
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
	if balances[block.GenesisAddress] != block.GenesisSupply {
		t.Errorf("genesis balance = %f, want %f", balances[block.GenesisAddress], block.GenesisSupply)
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
