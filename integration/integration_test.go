// Package integration provides end-to-end tests for the Noda cryptocurrency node.
//
// These tests exercise multiple packages together to validate:
//   - Mining + faucet + transfer flow
//   - Chain validation and serialization round-trips
//   - Tokenomics enforcement (faucet cap, mining cap)
//   - UTXO consistency across operations
package integration

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/Bihan293/Noda/block"
	"github.com/Bihan293/Noda/chain"
	"github.com/Bihan293/Noda/crypto"
	"github.com/Bihan293/Noda/ledger"
	"github.com/Bihan293/Noda/mempool"
	"github.com/Bihan293/Noda/utxo"
)

func tmpFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.json")
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Mining + Block Addition
// ──────────────────────────────────────────────────────────────────────────────

func TestMiningEndToEnd(t *testing.T) {
	bc := chain.NewBlockchain()

	// Mine 3 blocks.
	for i := uint64(1); i <= 3; i++ {
		reward := bc.GetBlockReward()
		tx := block.NewCoinbaseTx("miner_addr", reward, i)
		merkle := block.ComputeMerkleRoot([]string{tx.ID})
		target := bc.GetTarget()

		b := &block.Block{
			Header: block.BlockHeader{
				Version:       block.BlockVersion,
				Height:        i,
				PrevBlockHash: bc.LastHash(),
				MerkleRoot:    merkle,
				Timestamp:     bc.LastBlock().Header.Timestamp + 600,
			},
			Transactions: []block.Transaction{tx},
		}

		if err := block.MineBlock(b, target, 10_000_000); err != nil {
			t.Fatalf("MineBlock(%d) error: %v", i, err)
		}
		if err := bc.AddBlock(b); err != nil {
			t.Fatalf("AddBlock(%d) error: %v", i, err)
		}
	}

	if bc.Height() != 3 {
		t.Errorf("Height() = %d, want 3", bc.Height())
	}
	if bc.TotalMined != 150 { // 3 * 50
		t.Errorf("TotalMined = %f, want 150", bc.TotalMined)
	}

	// Validate the chain.
	if err := chain.ValidateChain(bc); err != nil {
		t.Errorf("ValidateChain() error: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Chain Serialization Round-Trip
// ──────────────────────────────────────────────────────────────────────────────

func TestChainSerializationRoundTrip(t *testing.T) {
	bc := chain.NewBlockchain()

	// Mine a block.
	tx := block.NewCoinbaseTx("miner", 50, 1)
	merkle := block.ComputeMerkleRoot([]string{tx.ID})
	target := bc.GetTarget()

	b := &block.Block{
		Header: block.BlockHeader{
			Version:       block.BlockVersion,
			Height:        1,
			PrevBlockHash: bc.LastHash(),
			MerkleRoot:    merkle,
			Timestamp:     bc.LastBlock().Header.Timestamp + 600,
		},
		Transactions: []block.Transaction{tx},
	}
	block.MineBlock(b, target, 10_000_000)
	bc.AddBlock(b)

	// Serialize.
	data, err := bc.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error: %v", err)
	}

	// Deserialize.
	bc2, err := chain.FromJSON(data)
	if err != nil {
		t.Fatalf("FromJSON() error: %v", err)
	}

	// Compare.
	if bc2.Len() != bc.Len() {
		t.Errorf("deserialized Len() = %d, want %d", bc2.Len(), bc.Len())
	}
	if bc2.Height() != bc.Height() {
		t.Errorf("deserialized Height() = %d, want %d", bc2.Height(), bc.Height())
	}
	if bc2.TotalMined != bc.TotalMined {
		t.Errorf("deserialized TotalMined = %f, want %f", bc2.TotalMined, bc.TotalMined)
	}

	// Validate deserialized chain.
	if err := chain.ValidateChain(bc2); err != nil {
		t.Errorf("ValidateChain(deserialized) error: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: UTXO Consistency
// ──────────────────────────────────────────────────────────────────────────────

func TestUTXOConsistency(t *testing.T) {
	// Build a chain with genesis + 2 mined blocks.
	genesis := block.NewGenesisBlock()
	blocks := []*block.Block{genesis}

	// Mine block 1: coinbase 50 to miner.
	tx1 := block.NewCoinbaseTx("miner", 50, 1)
	merkle1 := block.ComputeMerkleRoot([]string{tx1.ID})
	easyTarget := new(big.Int)
	easyTarget.SetString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)

	b1 := &block.Block{
		Header: block.BlockHeader{
			Version:       block.BlockVersion,
			Height:        1,
			PrevBlockHash: genesis.Hash,
			MerkleRoot:    merkle1,
			Timestamp:     genesis.Header.Timestamp + 600,
		},
		Transactions: []block.Transaction{tx1},
	}
	block.MineBlock(b1, easyTarget, 100)
	blocks = append(blocks, b1)

	// Rebuild UTXO set.
	utxoSet, err := utxo.RebuildFromBlocks(blocks)
	if err != nil {
		t.Fatalf("RebuildFromBlocks() error: %v", err)
	}

	// Genesis address should have 11M.
	genesisBalance := utxoSet.Balance(block.GenesisAddress)
	if genesisBalance != block.GenesisSupply {
		t.Errorf("genesis balance = %f, want %f", genesisBalance, block.GenesisSupply)
	}

	// Miner should have 50.
	minerBalance := utxoSet.Balance("miner")
	if minerBalance != 50 {
		t.Errorf("miner balance = %f, want 50", minerBalance)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Crypto Sign + Verify
// ──────────────────────────────────────────────────────────────────────────────

func TestSignVerifyIntegration(t *testing.T) {
	// Generate key pair.
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	privHex := hex.EncodeToString(kp.PrivateKey)
	to := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	amount := 42.5

	// Sign a transaction.
	sig, err := crypto.SignTransaction(privHex, kp.Address, to, amount)
	if err != nil {
		t.Fatal(err)
	}

	// Verify using the same message format as the ledger.
	msg := fmt.Sprintf("%s:%s:%f", kp.Address, to, amount)
	if !crypto.Verify(kp.Address, []byte(msg), sig) {
		t.Error("signature verification failed")
	}

	// Derive address from private key.
	addr, err := crypto.AddressFromPrivateKey(privHex)
	if err != nil {
		t.Fatal(err)
	}
	if addr != kp.Address {
		t.Errorf("derived address = %s, want %s", addr, kp.Address)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Mempool + Block Confirmation
// ──────────────────────────────────────────────────────────────────────────────

func TestMempoolBlockConfirmation(t *testing.T) {
	mp := mempool.New(100)

	// Add 3 transactions.
	for i := 0; i < 3; i++ {
		tx := block.Transaction{
			ID:        fmt.Sprintf("tx%d", i),
			From:      "alice",
			To:        "bob",
			Amount:    float64(10 + i),
			Timestamp: time.Now().Unix(),
			Signature: "sig",
		}
		mp.Add(tx)
	}

	if mp.Size() != 3 {
		t.Errorf("mempool size = %d, want 3", mp.Size())
	}

	// Simulate block confirmation — remove confirmed TXs.
	mp.RemoveBatch([]string{"tx0", "tx2"})

	if mp.Size() != 1 {
		t.Errorf("mempool size after confirmation = %d, want 1", mp.Size())
	}
	if !mp.Has("tx1") {
		t.Error("tx1 should still be pending")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Faucet Cap Enforcement
// ──────────────────────────────────────────────────────────────────────────────

func TestFaucetConstants(t *testing.T) {
	// Verify faucet constants match the tokenomics.
	if ledger.FaucetAmount != 5000 {
		t.Errorf("FaucetAmount = %f, want 5000", ledger.FaucetAmount)
	}
	if ledger.FaucetGlobalCap != 11_000_000 {
		t.Errorf("FaucetGlobalCap = %f, want 11000000", ledger.FaucetGlobalCap)
	}
	if block.GenesisSupply != ledger.FaucetGlobalCap {
		t.Errorf("GenesisSupply(%f) != FaucetGlobalCap(%f)", block.GenesisSupply, ledger.FaucetGlobalCap)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Ledger Save / Load
// ──────────────────────────────────────────────────────────────────────────────

func TestLedgerPersistence(t *testing.T) {
	path := tmpFile(t)

	// Create and save.
	l1 := ledger.NewLedger(path)
	if err := l1.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Load.
	l2 := ledger.LoadLedger(path)
	if l2.GetChainHeight() != l1.GetChainHeight() {
		t.Errorf("loaded height = %d, want %d", l2.GetChainHeight(), l1.GetChainHeight())
	}

	// Balances should match.
	b1 := l1.GetBalance(block.GenesisAddress)
	b2 := l2.GetBalance(block.GenesisAddress)
	if b1 != b2 {
		t.Errorf("genesis balance: saved=%f loaded=%f", b1, b2)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Tokenomics Verification
// ──────────────────────────────────────────────────────────────────────────────

func TestTokenomics(t *testing.T) {
	// 1. Total supply = Genesis + Mining = 21M.
	if block.GenesisSupply+block.MaxMiningSupply != block.MaxTotalSupply {
		t.Errorf("Genesis(%f) + Mining(%f) != Total(%f)",
			block.GenesisSupply, block.MaxMiningSupply, block.MaxTotalSupply)
	}

	// 2. Faucet cap = Genesis supply = 11M.
	if ledger.FaucetGlobalCap != block.GenesisSupply {
		t.Errorf("FaucetGlobalCap(%f) != GenesisSupply(%f)",
			ledger.FaucetGlobalCap, block.GenesisSupply)
	}

	// 3. Initial block reward = 50.
	if block.InitialBlockReward != 50 {
		t.Errorf("InitialBlockReward = %f, want 50", block.InitialBlockReward)
	}

	// 4. Halving interval = 210000.
	if block.HalvingInterval != 210_000 {
		t.Errorf("HalvingInterval = %d, want 210000", block.HalvingInterval)
	}

	// 5. Mining rewards sum test (verify first few eras).
	// Era 0: 210000 * 50 = 10,500,000 (but capped at 10M).
	totalMiningRewards := 0.0
	for h := uint64(0); h < 10*block.HalvingInterval; h++ {
		reward := block.BlockReward(h, totalMiningRewards)
		if reward == 0 {
			break
		}
		totalMiningRewards += reward
	}
	// Mining rewards should approach 10M.
	if totalMiningRewards > block.MaxMiningSupply {
		t.Errorf("total mining rewards = %f, exceeds %f", totalMiningRewards, block.MaxMiningSupply)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Full Transaction Flow
// ──────────────────────────────────────────────────────────────────────────────

func TestFullTransactionFlow(t *testing.T) {
	l := ledger.NewLedger(tmpFile(t))

	// Generate keys for sender (use genesis address for simplicity, we sign with a test key).
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sender := hex.EncodeToString(pub)
	privHex := hex.EncodeToString(priv)

	// Fund the sender by creating a UTXO manually.
	fundOp := utxo.OutPoint{TxID: "fund_tx", Index: 0}
	fundOut := utxo.Output{Address: sender, Amount: 1000}
	l.UTXOSet.Add(fundOp, fundOut)

	// Create recipient.
	recvKP, _ := crypto.GenerateKeyPair()
	receiver := recvKP.Address

	// Sign a transaction.
	amount := 100.0
	sig, err := crypto.SignTransaction(privHex, sender, receiver, amount)
	if err != nil {
		t.Fatalf("SignTransaction() error: %v", err)
	}

	// Verify the signature.
	msg := fmt.Sprintf("%s:%s:%f", sender, receiver, amount)
	if !crypto.Verify(sender, []byte(msg), sig) {
		t.Error("Verify() failed for valid signature")
	}

	// Validate the transaction through the ledger.
	tx := block.Transaction{
		From:      sender,
		To:        receiver,
		Amount:    amount,
		Signature: sig,
	}
	err = l.ValidateUserTx(tx)
	if err != nil {
		t.Fatalf("ValidateUserTx() error: %v", err)
	}
}
