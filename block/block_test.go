package block

import (
	"fmt"
	"math/big"
	"testing"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

func TestConstants(t *testing.T) {
	if GenesisSupply != 11_000_000 {
		t.Errorf("GenesisSupply = %f, want 11000000", GenesisSupply)
	}
	if MaxTotalSupply != 21_000_000 {
		t.Errorf("MaxTotalSupply = %f, want 21000000", MaxTotalSupply)
	}
	if MaxMiningSupply != 10_000_000 {
		t.Errorf("MaxMiningSupply = %f, want 10000000", MaxMiningSupply)
	}
	if InitialBlockReward != 50.0 {
		t.Errorf("InitialBlockReward = %f, want 50", InitialBlockReward)
	}
	if HalvingInterval != 210_000 {
		t.Errorf("HalvingInterval = %d, want 210000", HalvingInterval)
	}
	if DifficultyAdjustmentInterval != 2016 {
		t.Errorf("DifficultyAdjustmentInterval = %d, want 2016", DifficultyAdjustmentInterval)
	}
	if TargetBlockTime != 10*time.Minute {
		t.Errorf("TargetBlockTime = %v, want 10m", TargetBlockTime)
	}
	// Verify tokenomics: genesis + mining = total.
	if GenesisSupply+MaxMiningSupply != MaxTotalSupply {
		t.Errorf("GenesisSupply + MaxMiningSupply = %f, want %f", GenesisSupply+MaxMiningSupply, MaxTotalSupply)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// HashTransaction
// ──────────────────────────────────────────────────────────────────────────────

func TestHashTransaction(t *testing.T) {
	tx := Transaction{
		From:      "alice",
		To:        "bob",
		Amount:    100,
		Timestamp: 1000,
		Signature: "sig",
	}
	hash1 := HashTransaction(tx)
	if hash1 == "" {
		t.Fatal("HashTransaction() returned empty string")
	}
	if len(hash1) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash1))
	}

	// Same input must produce same hash.
	hash2 := HashTransaction(tx)
	if hash1 != hash2 {
		t.Error("HashTransaction() is not deterministic")
	}

	// Different input must produce different hash.
	tx.Amount = 200
	hash3 := HashTransaction(tx)
	if hash1 == hash3 {
		t.Error("HashTransaction() returned same hash for different input")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Merkle Tree
// ──────────────────────────────────────────────────────────────────────────────

func TestComputeMerkleRoot_Empty(t *testing.T) {
	root := ComputeMerkleRoot(nil)
	if root == "" {
		t.Fatal("ComputeMerkleRoot(nil) returned empty string")
	}
	if len(root) != 64 {
		t.Errorf("root length = %d, want 64", len(root))
	}
}

func TestComputeMerkleRoot_SingleTx(t *testing.T) {
	txID := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	root := ComputeMerkleRoot([]string{txID})
	if root == "" {
		t.Fatal("ComputeMerkleRoot(1 tx) returned empty string")
	}
}

func TestComputeMerkleRoot_TwoTxs(t *testing.T) {
	ids := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	root := ComputeMerkleRoot(ids)
	if root == "" {
		t.Fatal("ComputeMerkleRoot(2 txs) returned empty string")
	}
}

func TestComputeMerkleRoot_OddTxs(t *testing.T) {
	ids := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	root := ComputeMerkleRoot(ids)
	if root == "" {
		t.Fatal("ComputeMerkleRoot(3 txs) returned empty string")
	}
	if len(root) != 64 {
		t.Errorf("root length = %d, want 64", len(root))
	}
}

func TestComputeMerkleRoot_Deterministic(t *testing.T) {
	ids := []string{"aabb", "ccdd", "eeff"}
	r1 := ComputeMerkleRoot(ids)
	r2 := ComputeMerkleRoot(ids)
	if r1 != r2 {
		t.Error("ComputeMerkleRoot() is not deterministic")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Block Hashing & PoW
// ──────────────────────────────────────────────────────────────────────────────

func TestHashBlockHeader_Deterministic(t *testing.T) {
	h := BlockHeader{
		Version:       1,
		Height:        5,
		PrevBlockHash: "0000000000000000000000000000000000000000000000000000000000000000",
		MerkleRoot:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Timestamp:     1000,
		Bits:          "00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Nonce:         42,
	}
	hash1 := HashBlockHeader(h)
	hash2 := HashBlockHeader(h)
	if hash1 != hash2 {
		t.Error("HashBlockHeader() is not deterministic")
	}
	if len(hash1) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash1))
	}
}

func TestBitsAndTarget_RoundTrip(t *testing.T) {
	target := new(big.Int)
	target.SetString("00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)

	bits := BitsFromTarget(target)
	recovered := TargetFromBits(bits)

	if target.Cmp(recovered) != 0 {
		t.Errorf("target round-trip failed: %s != %s", target, recovered)
	}
}

func TestMeetsTarget(t *testing.T) {
	target := new(big.Int)
	target.SetString("00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)

	// Hash well below target.
	if !MeetsTarget("0000000000000000000000000000000000000000000000000000000000000001", target) {
		t.Error("MeetsTarget() should return true for hash below target")
	}

	// Hash above target.
	if MeetsTarget("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", target) {
		t.Error("MeetsTarget() should return false for hash above target")
	}
}

func TestMineBlock(t *testing.T) {
	// Use a very easy target for fast mining in tests.
	easyTarget := new(big.Int)
	easyTarget.SetString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)

	tx := Transaction{
		From:      "",
		To:        "miner",
		Amount:    50,
		Timestamp: time.Now().Unix(),
		Signature: "coinbase:1",
	}
	tx.ID = HashTransaction(tx)

	b := &Block{
		Header: BlockHeader{
			Version:       BlockVersion,
			Height:        1,
			PrevBlockHash: "0000000000000000000000000000000000000000000000000000000000000000",
			MerkleRoot:    ComputeMerkleRoot([]string{tx.ID}),
			Timestamp:     time.Now().Unix(),
		},
		Transactions: []Transaction{tx},
	}

	err := MineBlock(b, easyTarget, 100)
	if err != nil {
		t.Fatalf("MineBlock() error: %v", err)
	}
	if b.Hash == "" {
		t.Error("MineBlock() did not set hash")
	}
	if !MeetsTarget(b.Hash, easyTarget) {
		t.Error("MineBlock() hash does not meet target")
	}
}

func TestMineBlock_ExhaustedAttempts(t *testing.T) {
	// Impossible target.
	impossibleTarget := big.NewInt(0)

	b := &Block{
		Header: BlockHeader{
			Version:       BlockVersion,
			Height:        1,
			PrevBlockHash: "0000000000000000000000000000000000000000000000000000000000000000",
			MerkleRoot:    "aaaa",
			Timestamp:     time.Now().Unix(),
		},
		Transactions: []Transaction{{ID: "tx1"}},
	}

	err := MineBlock(b, impossibleTarget, 10)
	if err == nil {
		t.Error("MineBlock() should fail with impossible target")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Block Reward & Halving
// ──────────────────────────────────────────────────────────────────────────────

func TestBlockReward_Initial(t *testing.T) {
	reward := BlockReward(1, 0)
	if reward != 50.0 {
		t.Errorf("BlockReward(1, 0) = %f, want 50", reward)
	}
}

func TestBlockReward_FirstHalving(t *testing.T) {
	reward := BlockReward(HalvingInterval, 0)
	if reward != 25.0 {
		t.Errorf("BlockReward(%d, 0) = %f, want 25", HalvingInterval, reward)
	}
}

func TestBlockReward_SecondHalving(t *testing.T) {
	reward := BlockReward(2*HalvingInterval, 0)
	if reward != 12.5 {
		t.Errorf("BlockReward(%d, 0) = %f, want 12.5", 2*HalvingInterval, reward)
	}
}

func TestBlockReward_CapExceeded(t *testing.T) {
	// Already mined all 10M.
	reward := BlockReward(1, MaxMiningSupply)
	if reward != 0 {
		t.Errorf("BlockReward with full supply = %f, want 0", reward)
	}
}

func TestBlockReward_PartialRemaining(t *testing.T) {
	// Only 10 coins remaining.
	reward := BlockReward(1, MaxMiningSupply-10)
	if reward != 10 {
		t.Errorf("BlockReward with 10 remaining = %f, want 10", reward)
	}
}

func TestBlockReward_ManyHalvings(t *testing.T) {
	reward := BlockReward(64*HalvingInterval, 0)
	if reward != 0 {
		t.Errorf("BlockReward(64 halvings) = %f, want 0", reward)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Difficulty Adjustment
// ──────────────────────────────────────────────────────────────────────────────

func TestAdjustDifficulty_TooFast(t *testing.T) {
	// Blocks too fast → lower target (harder difficulty).
	expectedSpan := int64(DifficultyAdjustmentInterval) * int64(TargetBlockTime.Seconds())
	actualSpan := expectedSpan / 2 // twice as fast

	newTarget := AdjustDifficulty(InitialTarget, actualSpan)

	if newTarget.Cmp(InitialTarget) >= 0 {
		t.Error("AdjustDifficulty() should lower target (harder) when blocks are fast")
	}
}

func TestAdjustDifficulty_TooSlow(t *testing.T) {
	// Blocks too slow → higher target (easier difficulty).
	expectedSpan := int64(DifficultyAdjustmentInterval) * int64(TargetBlockTime.Seconds())
	actualSpan := expectedSpan * 2 // twice as slow

	currentTarget := new(big.Int).Div(InitialTarget, big.NewInt(10))
	newTarget := AdjustDifficulty(currentTarget, actualSpan)

	if newTarget.Cmp(currentTarget) <= 0 {
		t.Error("AdjustDifficulty() should raise target (easier) when blocks are slow")
	}
}

func TestAdjustDifficulty_ClampedMax(t *testing.T) {
	// Extremely slow blocks → clamped to 4x.
	expectedSpan := int64(DifficultyAdjustmentInterval) * int64(TargetBlockTime.Seconds())
	actualSpan := expectedSpan * 100 // way too slow

	currentTarget := new(big.Int).Div(InitialTarget, big.NewInt(100))
	newTarget := AdjustDifficulty(currentTarget, actualSpan)

	maxAllowed := new(big.Int).Mul(currentTarget, big.NewInt(int64(MaxDifficultyAdjustmentFactor)))
	if newTarget.Cmp(maxAllowed) > 0 && newTarget.Cmp(InitialTarget) > 0 {
		t.Error("AdjustDifficulty() should be clamped to 4x")
	}
}

func TestAdjustDifficulty_NeverExceedsInitial(t *testing.T) {
	// Even with very slow blocks, target should not exceed InitialTarget.
	expectedSpan := int64(DifficultyAdjustmentInterval) * int64(TargetBlockTime.Seconds())
	actualSpan := expectedSpan * 100

	newTarget := AdjustDifficulty(InitialTarget, actualSpan)

	if newTarget.Cmp(InitialTarget) > 0 {
		t.Error("AdjustDifficulty() should never exceed InitialTarget")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Coinbase Transaction
// ──────────────────────────────────────────────────────────────────────────────

func TestNewCoinbaseTx(t *testing.T) {
	tx := NewCoinbaseTx("miner_address", 50, 10)

	if tx.From != "" {
		t.Errorf("coinbase From = %q, want empty", tx.From)
	}
	if tx.To != "miner_address" {
		t.Errorf("coinbase To = %q, want miner_address", tx.To)
	}
	if tx.Amount != 50 {
		t.Errorf("coinbase Amount = %f, want 50", tx.Amount)
	}
	expectedSig := fmt.Sprintf("coinbase:%d", 10)
	if tx.Signature != expectedSig {
		t.Errorf("coinbase Signature = %q, want %q", tx.Signature, expectedSig)
	}
	if tx.ID == "" {
		t.Error("coinbase ID is empty")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Genesis Block
// ──────────────────────────────────────────────────────────────────────────────

func TestNewGenesisBlock(t *testing.T) {
	genesis := NewGenesisBlock()

	if genesis == nil {
		t.Fatal("NewGenesisBlock() returned nil")
	}
	if genesis.Header.Height != 0 {
		t.Errorf("genesis height = %d, want 0", genesis.Header.Height)
	}
	if genesis.Header.PrevBlockHash != "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Error("genesis PrevBlockHash is not all zeros")
	}
	if genesis.Hash == "" {
		t.Error("genesis hash is empty")
	}
	if len(genesis.Transactions) != 1 {
		t.Errorf("genesis TX count = %d, want 1", len(genesis.Transactions))
	}
	if genesis.Transactions[0].Amount != GenesisSupply {
		t.Errorf("genesis TX amount = %f, want %f", genesis.Transactions[0].Amount, GenesisSupply)
	}
	if genesis.Transactions[0].To != GenesisAddress {
		t.Errorf("genesis TX to = %s, want %s", genesis.Transactions[0].To, GenesisAddress)
	}
	if genesis.Transactions[0].Signature != "genesis" {
		t.Errorf("genesis TX signature = %q, want %q", genesis.Transactions[0].Signature, "genesis")
	}
}

func TestGenesisBlockDeterministic(t *testing.T) {
	g1 := NewGenesisBlock()
	g2 := NewGenesisBlock()

	if g1.Hash != g2.Hash {
		t.Error("NewGenesisBlock() is not deterministic")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Block Validation
// ──────────────────────────────────────────────────────────────────────────────

func TestValidateBlockHeader_Genesis(t *testing.T) {
	genesis := NewGenesisBlock()
	err := ValidateBlockHeader(genesis,
		"0000000000000000000000000000000000000000000000000000000000000000", 0)
	if err != nil {
		t.Errorf("ValidateBlockHeader(genesis) error: %v", err)
	}
}

func TestValidateBlockHeader_WrongHeight(t *testing.T) {
	genesis := NewGenesisBlock()
	err := ValidateBlockHeader(genesis,
		"0000000000000000000000000000000000000000000000000000000000000000", 5)
	if err == nil {
		t.Error("ValidateBlockHeader() should fail with wrong height")
	}
}

func TestValidateBlockHeader_WrongPrevHash(t *testing.T) {
	genesis := NewGenesisBlock()
	err := ValidateBlockHeader(genesis, "deadbeef", 0)
	if err == nil {
		t.Error("ValidateBlockHeader() should fail with wrong prev hash")
	}
}

func TestValidateBlockMerkle(t *testing.T) {
	genesis := NewGenesisBlock()
	err := ValidateBlockMerkle(genesis)
	if err != nil {
		t.Errorf("ValidateBlockMerkle(genesis) error: %v", err)
	}
}

func TestValidateBlockMerkle_Tampered(t *testing.T) {
	genesis := NewGenesisBlock()
	genesis.Header.MerkleRoot = "wrong_merkle_root_here"
	err := ValidateBlockMerkle(genesis)
	if err == nil {
		t.Error("ValidateBlockMerkle() should fail with wrong Merkle root")
	}
}

func TestValidateBlock_Genesis(t *testing.T) {
	genesis := NewGenesisBlock()
	err := ValidateBlock(genesis,
		"0000000000000000000000000000000000000000000000000000000000000000", 0)
	if err != nil {
		t.Errorf("ValidateBlock(genesis) error: %v", err)
	}
}

func TestValidateBlock_NoTransactions(t *testing.T) {
	b := &Block{
		Header: BlockHeader{
			Version:       BlockVersion,
			Height:        0,
			PrevBlockHash: "0000000000000000000000000000000000000000000000000000000000000000",
			MerkleRoot:    ComputeMerkleRoot(nil),
			Timestamp:     time.Now().Unix(),
		},
		Transactions: []Transaction{},
	}
	b.Hash = HashBlockHeader(b.Header)

	err := ValidateBlock(b,
		"0000000000000000000000000000000000000000000000000000000000000000", 0)
	if err == nil {
		t.Error("ValidateBlock() should fail for block with no transactions")
	}
}
