// Package block implements Bitcoin-like block structures with Proof of Work,
// Merkle Tree, dynamic difficulty adjustment and halving reward schedule.
//
// Tokenomics:
//   - Genesis supply: 11,000,000 coins (distributed via faucet)
//   - Initial block reward: 50 coins
//   - Halving interval: every 210,000 blocks
//   - Max mining supply: 10,000,000 coins
//   - Max total supply: 21,000,000 coins (11M faucet + 10M mining)
//   - Difficulty adjustment: every 2016 blocks, target 10 min/block
package block

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	// GenesisSupply is the total supply minted at genesis (distributed via faucet).
	GenesisSupply float64 = 11_000_000

	// MaxTotalSupply is the absolute maximum coins that can ever exist.
	MaxTotalSupply float64 = 21_000_000

	// MaxMiningSupply is the maximum coins that can be created through mining.
	MaxMiningSupply float64 = 10_000_000

	// InitialBlockReward is the coinbase reward for the first era.
	InitialBlockReward float64 = 50.0

	// HalvingInterval is the number of blocks between reward halvings.
	HalvingInterval uint64 = 210_000

	// DifficultyAdjustmentInterval is the number of blocks between difficulty recalculations.
	DifficultyAdjustmentInterval uint64 = 2016

	// TargetBlockTime is the desired average time between blocks.
	TargetBlockTime = 10 * time.Minute

	// MaxDifficultyAdjustmentFactor limits how much difficulty can change in one adjustment.
	MaxDifficultyAdjustmentFactor = 4.0

	// GenesisAddress is the well-known address that holds the genesis supply.
	GenesisAddress = "8fdc70be14ada0e514953b00e9148df9ba6207233d72b4c8e4f8cbd275c181de"

	// BlockVersion is the current block format version.
	BlockVersion uint32 = 1
)

// InitialTarget is the starting difficulty target (relatively easy for development).
// In production this would be calibrated for the expected hash rate.
// This represents roughly 2^236 — easy enough for CPU mining.
var InitialTarget *big.Int

func init() {
	InitialTarget = new(big.Int)
	// Start with a moderate difficulty: leading 2 zero-bytes (0x00ff...)
	InitialTarget.SetString("00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)
}

// ──────────────────────────────────────────────────────────────────────────────
// Transaction (carried inside blocks)
// ──────────────────────────────────────────────────────────────────────────────

// Transaction represents a single transfer of coins.
// Coinbase transactions have From="" and Signature="coinbase".
type Transaction struct {
	ID        string  `json:"id"`        // SHA-256 hash of content
	From      string  `json:"from"`      // sender address (empty for coinbase)
	To        string  `json:"to"`        // receiver address
	Amount    float64 `json:"amount"`    // coin amount (> 0)
	Timestamp int64   `json:"timestamp"` // unix timestamp
	Signature string  `json:"signature"` // hex Ed25519 signature (or "coinbase"/"genesis")
}

// HashTransaction computes the SHA-256 hash of a transaction's content.
func HashTransaction(tx Transaction) string {
	record := fmt.Sprintf("%s:%s:%f:%d:%s",
		tx.From, tx.To, tx.Amount, tx.Timestamp, tx.Signature)
	h := sha256.Sum256([]byte(record))
	return hex.EncodeToString(h[:])
}

// ──────────────────────────────────────────────────────────────────────────────
// Block Header & Block
// ──────────────────────────────────────────────────────────────────────────────

// BlockHeader contains all metadata about a block.
type BlockHeader struct {
	Version       uint32 `json:"version"`         // block format version
	Height        uint64 `json:"height"`          // block number (0 = genesis)
	PrevBlockHash string `json:"prev_block_hash"` // hash of the previous block header
	MerkleRoot    string `json:"merkle_root"`     // root of the Merkle tree of transactions
	Timestamp     int64  `json:"timestamp"`       // unix timestamp when block was mined
	Bits          string `json:"bits"`            // compact target representation (hex of target)
	Nonce         uint64 `json:"nonce"`           // PoW nonce
}

// Block is a complete block containing a header and a list of transactions.
type Block struct {
	Header       BlockHeader   `json:"header"`
	Transactions []Transaction `json:"transactions"`
	Hash         string        `json:"hash"` // SHA-256 double-hash of the header
}

// ──────────────────────────────────────────────────────────────────────────────
// Merkle Tree
// ──────────────────────────────────────────────────────────────────────────────

// ComputeMerkleRoot computes the binary Merkle tree root hash from transaction IDs.
// If the list is empty, returns a hash of empty string.
// If the list has an odd number, the last element is duplicated.
func ComputeMerkleRoot(txIDs []string) string {
	if len(txIDs) == 0 {
		h := sha256.Sum256([]byte(""))
		return hex.EncodeToString(h[:])
	}

	// Start with transaction hashes as leaf nodes.
	level := make([][]byte, len(txIDs))
	for i, id := range txIDs {
		b, err := hex.DecodeString(id)
		if err != nil {
			// If ID is not valid hex, hash the string directly.
			h := sha256.Sum256([]byte(id))
			level[i] = h[:]
		} else {
			level[i] = b
		}
	}

	// Build tree bottom-up.
	for len(level) > 1 {
		// Duplicate last element if odd.
		if len(level)%2 != 0 {
			level = append(level, level[len(level)-1])
		}

		nextLevel := make([][]byte, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			combined := append(level[i], level[i+1]...)
			h := doubleSHA256(combined)
			nextLevel[i/2] = h[:]
		}
		level = nextLevel
	}

	return hex.EncodeToString(level[0])
}

// ──────────────────────────────────────────────────────────────────────────────
// Hashing & Proof of Work
// ──────────────────────────────────────────────────────────────────────────────

// doubleSHA256 computes SHA-256(SHA-256(data)), the Bitcoin-style double hash.
func doubleSHA256(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

// HashBlockHeader computes the double-SHA-256 hash of a block header.
func HashBlockHeader(h BlockHeader) string {
	// Serialize header fields into a deterministic byte sequence.
	data := serializeHeader(h)
	hash := doubleSHA256(data)
	return hex.EncodeToString(hash[:])
}

// serializeHeader converts a BlockHeader into a deterministic byte slice for hashing.
func serializeHeader(h BlockHeader) []byte {
	buf := make([]byte, 0, 256)

	// Version (4 bytes, little-endian)
	vBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(vBuf, h.Version)
	buf = append(buf, vBuf...)

	// Height (8 bytes, little-endian)
	hBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(hBuf, h.Height)
	buf = append(buf, hBuf...)

	// PrevBlockHash (raw bytes)
	prevBytes, _ := hex.DecodeString(h.PrevBlockHash)
	buf = append(buf, prevBytes...)

	// MerkleRoot (raw bytes)
	merkleBytes, _ := hex.DecodeString(h.MerkleRoot)
	buf = append(buf, merkleBytes...)

	// Timestamp (8 bytes, little-endian)
	tBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(tBuf, uint64(h.Timestamp))
	buf = append(buf, tBuf...)

	// Bits (raw bytes of target hex)
	buf = append(buf, []byte(h.Bits)...)

	// Nonce (8 bytes, little-endian)
	nBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(nBuf, h.Nonce)
	buf = append(buf, nBuf...)

	return buf
}

// TargetFromBits parses the hex-encoded target string into a big.Int.
func TargetFromBits(bits string) *big.Int {
	t := new(big.Int)
	t.SetString(bits, 16)
	return t
}

// BitsFromTarget converts a big.Int target back to its hex string representation.
func BitsFromTarget(target *big.Int) string {
	return fmt.Sprintf("%064x", target)
}

// MeetsTarget checks whether the given block hash satisfies the target.
// The hash (as a big-endian number) must be <= target.
func MeetsTarget(hashHex string, target *big.Int) bool {
	hashInt := new(big.Int)
	hashInt.SetString(hashHex, 16)
	return hashInt.Cmp(target) <= 0
}

// MineBlock performs Proof of Work mining on the given block.
// It increments the nonce until the block hash meets the target or maxAttempts is reached.
// Returns the mined block with hash set, or an error if maxAttempts was exhausted.
func MineBlock(b *Block, target *big.Int, maxAttempts uint64) error {
	b.Header.Bits = BitsFromTarget(target)

	for nonce := uint64(0); nonce < maxAttempts; nonce++ {
		b.Header.Nonce = nonce
		hash := HashBlockHeader(b.Header)
		if MeetsTarget(hash, target) {
			b.Hash = hash
			return nil
		}
	}
	return fmt.Errorf("mining failed: exhausted %d attempts", maxAttempts)
}

// ──────────────────────────────────────────────────────────────────────────────
// Block Reward & Halving
// ──────────────────────────────────────────────────────────────────────────────

// BlockReward calculates the mining reward for a given block height.
// Reward starts at InitialBlockReward and halves every HalvingInterval blocks.
// Returns 0 once the reward has been halved below the minimum representable amount,
// or once total mined supply would exceed MaxMiningSupply.
func BlockReward(height uint64, totalMined float64) float64 {
	halvings := height / HalvingInterval

	// After 64 halvings the reward is effectively zero.
	if halvings >= 64 {
		return 0
	}

	reward := InitialBlockReward / math.Pow(2, float64(halvings))

	// Ensure we don't exceed the mining supply cap.
	remaining := MaxMiningSupply - totalMined
	if remaining <= 0 {
		return 0
	}
	if reward > remaining {
		reward = remaining
	}

	// Minimum reward threshold (below 1 satoshi-equivalent, we stop).
	if reward < 0.00000001 {
		return 0
	}

	return reward
}

// ──────────────────────────────────────────────────────────────────────────────
// Dynamic Difficulty Adjustment
// ──────────────────────────────────────────────────────────────────────────────

// AdjustDifficulty recalculates the target based on actual vs expected time span.
// Called every DifficultyAdjustmentInterval blocks.
//
// Parameters:
//   - currentTarget: the current difficulty target
//   - actualTimeSpan: actual seconds elapsed for the last 2016 blocks
//
// The adjustment is clamped to a factor of MaxDifficultyAdjustmentFactor in either direction.
func AdjustDifficulty(currentTarget *big.Int, actualTimeSpan int64) *big.Int {
	expectedTimeSpan := int64(DifficultyAdjustmentInterval) * int64(TargetBlockTime.Seconds())

	// Clamp the actual time span.
	minSpan := expectedTimeSpan / int64(MaxDifficultyAdjustmentFactor)
	maxSpan := expectedTimeSpan * int64(MaxDifficultyAdjustmentFactor)

	if actualTimeSpan < minSpan {
		actualTimeSpan = minSpan
	}
	if actualTimeSpan > maxSpan {
		actualTimeSpan = maxSpan
	}

	// newTarget = currentTarget * actualTimeSpan / expectedTimeSpan
	newTarget := new(big.Int).Set(currentTarget)
	newTarget.Mul(newTarget, big.NewInt(actualTimeSpan))
	newTarget.Div(newTarget, big.NewInt(expectedTimeSpan))

	// Don't let target exceed the initial (easiest) target.
	if newTarget.Cmp(InitialTarget) > 0 {
		newTarget.Set(InitialTarget)
	}

	// Don't let target go to zero.
	if newTarget.Sign() <= 0 {
		newTarget.SetInt64(1)
	}

	return newTarget
}

// ──────────────────────────────────────────────────────────────────────────────
// Coinbase Transaction
// ──────────────────────────────────────────────────────────────────────────────

// NewCoinbaseTx creates a coinbase (mining reward) transaction.
func NewCoinbaseTx(minerAddress string, reward float64, height uint64) Transaction {
	tx := Transaction{
		From:      "",
		To:        minerAddress,
		Amount:    reward,
		Timestamp: time.Now().Unix(),
		Signature: fmt.Sprintf("coinbase:%d", height),
	}
	tx.ID = HashTransaction(tx)
	return tx
}

// ──────────────────────────────────────────────────────────────────────────────
// Genesis Block
// ──────────────────────────────────────────────────────────────────────────────

// NewGenesisBlock creates the genesis block with the initial supply transaction.
// The genesis block has height 0, no previous hash, and a pre-set nonce/hash.
func NewGenesisBlock() *Block {
	// Genesis transaction: mint the entire faucet supply to the genesis address.
	genesisTx := Transaction{
		From:      "",
		To:        GenesisAddress,
		Amount:    GenesisSupply,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
		Signature: "genesis",
	}
	genesisTx.ID = HashTransaction(genesisTx)

	// Build the genesis block.
	txIDs := []string{genesisTx.ID}
	merkleRoot := ComputeMerkleRoot(txIDs)

	header := BlockHeader{
		Version:       BlockVersion,
		Height:        0,
		PrevBlockHash: "0000000000000000000000000000000000000000000000000000000000000000",
		MerkleRoot:    merkleRoot,
		Timestamp:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
		Bits:          BitsFromTarget(InitialTarget),
		Nonce:         0, // Genesis nonce is 0 (no PoW required for genesis).
	}

	block := &Block{
		Header:       header,
		Transactions: []Transaction{genesisTx},
	}

	// Compute hash (no PoW validation for genesis).
	block.Hash = HashBlockHeader(header)

	return block
}

// ──────────────────────────────────────────────────────────────────────────────
// Block Validation
// ──────────────────────────────────────────────────────────────────────────────

// ValidateBlockHeader checks the structural integrity of a block header.
//   - Hash matches the computed double-SHA-256 of the header.
//   - Hash meets the declared target (Bits).
//   - PrevBlockHash matches the expected value.
//   - Height is correct.
func ValidateBlockHeader(b *Block, expectedPrevHash string, expectedHeight uint64) error {
	// Check height.
	if b.Header.Height != expectedHeight {
		return fmt.Errorf("invalid height: expected %d, got %d", expectedHeight, b.Header.Height)
	}

	// Check prev hash.
	if b.Header.PrevBlockHash != expectedPrevHash {
		return fmt.Errorf("invalid prev_block_hash at height %d", b.Header.Height)
	}

	// Check computed hash.
	computed := HashBlockHeader(b.Header)
	if b.Hash != computed {
		return fmt.Errorf("hash mismatch at height %d: stored=%s computed=%s",
			b.Header.Height, b.Hash[:16], computed[:16])
	}

	// Skip PoW check for genesis block.
	if b.Header.Height == 0 {
		return nil
	}

	// Check PoW.
	target := TargetFromBits(b.Header.Bits)
	if !MeetsTarget(b.Hash, target) {
		return fmt.Errorf("PoW not satisfied at height %d", b.Header.Height)
	}

	return nil
}

// ValidateBlockMerkle verifies that the Merkle root in the header matches
// the transactions in the block body.
func ValidateBlockMerkle(b *Block) error {
	txIDs := make([]string, len(b.Transactions))
	for i, tx := range b.Transactions {
		txIDs[i] = tx.ID
	}
	computed := ComputeMerkleRoot(txIDs)
	if b.Header.MerkleRoot != computed {
		return fmt.Errorf("merkle root mismatch at height %d", b.Header.Height)
	}
	return nil
}

// ValidateBlock performs full validation of a block:
// header integrity, PoW, Merkle root, and basic transaction sanity.
func ValidateBlock(b *Block, expectedPrevHash string, expectedHeight uint64) error {
	if err := ValidateBlockHeader(b, expectedPrevHash, expectedHeight); err != nil {
		return err
	}
	if err := ValidateBlockMerkle(b); err != nil {
		return err
	}

	// Must have at least one transaction.
	if len(b.Transactions) == 0 {
		return fmt.Errorf("block at height %d has no transactions", b.Header.Height)
	}

	return nil
}
