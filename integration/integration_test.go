// Package integration provides end-to-end tests for the Noda cryptocurrency node.
//
// These tests exercise multiple packages together to validate:
//   - Mining + faucet + transfer flow
//   - Chain validation and serialization round-trips
//   - Tokenomics enforcement (faucet cap, mining cap)
//   - UTXO consistency across operations
//   - CRITICAL-2: explicit UTXO inputs/outputs in transactions
package integration

import (
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

	if bc2.Len() != bc.Len() {
		t.Errorf("deserialized Len() = %d, want %d", bc2.Len(), bc.Len())
	}
	if bc2.Height() != bc.Height() {
		t.Errorf("deserialized Height() = %d, want %d", bc2.Height(), bc.Height())
	}
	if bc2.TotalMined != bc.TotalMined {
		t.Errorf("deserialized TotalMined = %f, want %f", bc2.TotalMined, bc.TotalMined)
	}

	if err := chain.ValidateChain(bc2); err != nil {
		t.Errorf("ValidateChain(deserialized) error: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: UTXO Consistency
// ──────────────────────────────────────────────────────────────────────────────

func TestUTXOConsistency(t *testing.T) {
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

	genesisBalance := utxoSet.Balance(block.LegacyGenesisAddress)
	if genesisBalance != block.GenesisSupply {
		t.Errorf("genesis balance = %f, want %f", genesisBalance, block.GenesisSupply)
	}

	minerBalance := utxoSet.Balance("miner")
	if minerBalance != 50 {
		t.Errorf("miner balance = %f, want 50", minerBalance)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Crypto Sign + Verify (CRITICAL-2: sighash model)
// ──────────────────────────────────────────────────────────────────────────────

func TestSignVerifyIntegration(t *testing.T) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	privHex := hex.EncodeToString(kp.PrivateKey)
	to := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	amount := 42.5

	// Build a transaction to sign.
	tx := &block.Transaction{
		Version: block.TxVersion,
		Inputs: []block.TxInput{
			{PrevTxID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PrevIndex: 0, PubKey: kp.Address},
		},
		Outputs: []block.TxOutput{
			{Amount: amount, Address: to},
		},
	}

	// Compute sighash and sign.
	sighash := block.ComputeSighash(tx)
	sig, err := crypto.SignSighash(privHex, sighash)
	if err != nil {
		t.Fatal(err)
	}

	// Verify using sighash.
	if !crypto.VerifySighash(kp.Address, sighash, sig) {
		t.Error("sighash signature verification failed")
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
// End-to-End: Mempool + Block Confirmation (CRITICAL-2)
// ──────────────────────────────────────────────────────────────────────────────

func TestMempoolBlockConfirmation(t *testing.T) {
	mp := mempool.New(100)

	for i := 0; i < 3; i++ {
		tx := block.Transaction{
			ID:      fmt.Sprintf("tx%d", i),
			Version: block.TxVersion,
			Inputs: []block.TxInput{
				{PrevTxID: fmt.Sprintf("prev_%d", i), PrevIndex: 0, PubKey: "pk", Signature: "sig"},
			},
			Outputs: []block.TxOutput{
				{Amount: float64(10 + i), Address: "recipient"},
			},
		}
		mp.Add(tx)
	}

	if mp.Size() != 3 {
		t.Errorf("mempool size = %d, want 3", mp.Size())
	}

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

	l1 := ledger.NewLedger(path)
	if err := l1.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	l2 := ledger.LoadLedger(path)
	if l2.GetChainHeight() != l1.GetChainHeight() {
		t.Errorf("loaded height = %d, want %d", l2.GetChainHeight(), l1.GetChainHeight())
	}

	b1 := l1.GetBalance(block.LegacyGenesisAddress)
	b2 := l2.GetBalance(block.LegacyGenesisAddress)
	if b1 != b2 {
		t.Errorf("genesis balance: saved=%f loaded=%f", b1, b2)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Tokenomics Verification
// ──────────────────────────────────────────────────────────────────────────────

func TestTokenomics(t *testing.T) {
	if block.GenesisSupply+block.MaxMiningSupply != block.MaxTotalSupply {
		t.Errorf("Genesis(%f) + Mining(%f) != Total(%f)",
			block.GenesisSupply, block.MaxMiningSupply, block.MaxTotalSupply)
	}

	if ledger.FaucetGlobalCap != block.GenesisSupply {
		t.Errorf("FaucetGlobalCap(%f) != GenesisSupply(%f)",
			ledger.FaucetGlobalCap, block.GenesisSupply)
	}

	if block.InitialBlockReward != 50 {
		t.Errorf("InitialBlockReward = %f, want 50", block.InitialBlockReward)
	}

	if block.HalvingInterval != 210_000 {
		t.Errorf("HalvingInterval = %d, want 210000", block.HalvingInterval)
	}

	totalMiningRewards := 0.0
	for h := uint64(0); h < 10*block.HalvingInterval; h++ {
		reward := block.BlockReward(h, totalMiningRewards)
		if reward == 0 {
			break
		}
		totalMiningRewards += reward
	}
	if totalMiningRewards > block.MaxMiningSupply {
		t.Errorf("total mining rewards = %f, exceeds %f", totalMiningRewards, block.MaxMiningSupply)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// End-to-End: Full Transaction Flow (CRITICAL-2: wallet builder + UTXO)
// ──────────────────────────────────────────────────────────────────────────────

func TestFullTransactionFlow(t *testing.T) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privHex := hex.EncodeToString(kp.PrivateKey)

	// Create a ledger with this key as genesis owner.
	l := ledger.NewLedgerWithOwner(tmpFile(t), kp.Address)

	// Create recipient.
	recvKP, _ := crypto.GenerateKeyPair()
	receiver := recvKP.Address

	// Build a transaction using the wallet builder.
	amount := 100.0
	tx, err := l.BuildTransaction(privHex, kp.Address, receiver, amount)
	if err != nil {
		t.Fatalf("BuildTransaction() error: %v", err)
	}

	// Verify the transaction is well-formed.
	if len(tx.Inputs) == 0 {
		t.Error("tx should have inputs")
	}
	if len(tx.Outputs) < 1 {
		t.Error("tx should have at least one output")
	}
	if tx.ID == "" {
		t.Error("tx ID should be set")
	}

	// Validate through ledger.
	err = l.ValidateUserTx(*tx)
	if err != nil {
		t.Fatalf("ValidateUserTx() error: %v", err)
	}

	// Submit (which mines a block).
	err = l.SubmitTransaction(*tx)
	if err != nil {
		t.Fatalf("SubmitTransaction() error: %v", err)
	}

	// Verify balances.
	receiverBalance := l.GetBalance(receiver)
	if receiverBalance != amount {
		t.Errorf("receiver balance = %f, want %f", receiverBalance, amount)
	}

	senderBalance := l.GetBalance(kp.Address)
	expectedChange := block.GenesisSupply - amount
	if senderBalance != expectedChange {
		t.Errorf("sender balance = %f, want %f", senderBalance, expectedChange)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CRITICAL-2: Verify no From/To/Amount in consensus blocks
// ──────────────────────────────────────────────────────────────────────────────

func TestNoLegacyFieldsInBlocks(t *testing.T) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privHex := hex.EncodeToString(kp.PrivateKey)

	l := ledger.NewLedgerWithOwner(tmpFile(t), kp.Address)
	if err := l.SetFaucetKeyAndValidateGenesis(privHex); err != nil {
		t.Fatal(err)
	}

	recvKP, _ := crypto.GenerateKeyPair()
	_, err = l.ProcessFaucet(recvKP.Address)
	if err != nil {
		t.Fatalf("ProcessFaucet() error: %v", err)
	}

	// Inspect all transactions in all blocks.
	bc := l.GetChain()
	for _, b := range bc.Blocks {
		for _, tx := range b.Transactions {
			// Every tx must have explicit outputs.
			if len(tx.Outputs) == 0 {
				t.Errorf("block %d: tx %s has no outputs", b.Header.Height, tx.ID[:8])
			}
			// Non-coinbase txs must have explicit inputs.
			if !tx.IsCoinbase() && !tx.IsGenesis() {
				if len(tx.Inputs) == 0 {
					t.Errorf("block %d: regular tx %s has no inputs", b.Header.Height, tx.ID[:8])
				}
			}
		}
	}
}

// Suppress unused import warnings.
var _ = time.Now
