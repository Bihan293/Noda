// Package chain manages the blockchain — an ordered sequence of blocks.
// It provides methods to add blocks, query state, and serialize/deserialize
// the chain for persistence and P2P synchronization.
package chain

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"sync"

	"github.com/Bihan293/Noda/block"
)

// Blockchain is an ordered list of blocks forming the ledger.
type Blockchain struct {
	Blocks      []*block.Block `json:"blocks"`
	TotalMined  float64        `json:"total_mined"`  // total coins created via mining rewards
	TotalFaucet float64        `json:"total_faucet"`  // total coins distributed via faucet
	Target      *big.Int       `json:"-"`             // current difficulty target (not serialized directly)
	TargetHex   string         `json:"target_hex"`    // hex-serialized target for JSON persistence
	mu          sync.RWMutex
}

// NewBlockchain creates a new blockchain with the genesis block.
func NewBlockchain() *Blockchain {
	bc := &Blockchain{
		Blocks:    make([]*block.Block, 0),
		Target:    new(big.Int).Set(block.InitialTarget),
		TargetHex: block.BitsFromTarget(block.InitialTarget),
	}
	bc.addGenesis()
	return bc
}

// addGenesis appends the genesis block.
func (bc *Blockchain) addGenesis() {
	genesis := block.NewGenesisBlock()
	bc.Blocks = append(bc.Blocks, genesis)
	log.Printf("[CHAIN] Genesis block created — hash: %s", genesis.Hash[:16])
}

// Height returns the height of the last block (0-indexed).
func (bc *Blockchain) Height() uint64 {
	if len(bc.Blocks) == 0 {
		return 0
	}
	return bc.Blocks[len(bc.Blocks)-1].Header.Height
}

// Len returns the number of blocks in the chain.
func (bc *Blockchain) Len() int {
	return len(bc.Blocks)
}

// LastBlock returns the most recent block.
func (bc *Blockchain) LastBlock() *block.Block {
	if len(bc.Blocks) == 0 {
		return nil
	}
	return bc.Blocks[len(bc.Blocks)-1]
}

// LastHash returns the hash of the most recent block.
func (bc *Blockchain) LastHash() string {
	last := bc.LastBlock()
	if last == nil {
		return ""
	}
	return last.Hash
}

// GetBlock returns the block at the given height, or nil if out of range.
func (bc *Blockchain) GetBlock(height uint64) *block.Block {
	if height >= uint64(len(bc.Blocks)) {
		return nil
	}
	return bc.Blocks[height]
}

// AddBlock validates and appends a new block to the chain.
// It checks header integrity, PoW, Merkle root, and chain linkage.
func (bc *Blockchain) AddBlock(b *block.Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	expectedHeight := bc.Height() + 1
	expectedPrevHash := bc.LastHash()

	// Validate the block.
	if err := block.ValidateBlock(b, expectedPrevHash, expectedHeight); err != nil {
		return fmt.Errorf("block validation failed: %w", err)
	}

	// Track mined coins from coinbase transaction (first TX in block, if coinbase).
	if len(b.Transactions) > 0 && b.Transactions[0].From == "" && b.Header.Height > 0 {
		bc.TotalMined += b.Transactions[0].Amount
	}

	bc.Blocks = append(bc.Blocks, b)

	// Adjust difficulty if needed.
	if expectedHeight > 0 && expectedHeight%block.DifficultyAdjustmentInterval == 0 {
		bc.adjustDifficulty()
	}

	return nil
}

// adjustDifficulty recalculates the mining target based on actual block times.
func (bc *Blockchain) adjustDifficulty() {
	height := bc.Height()
	if height < block.DifficultyAdjustmentInterval {
		return
	}

	// Look back DifficultyAdjustmentInterval blocks.
	lastBlock := bc.Blocks[height]
	firstBlock := bc.Blocks[height-block.DifficultyAdjustmentInterval]

	actualTimeSpan := lastBlock.Header.Timestamp - firstBlock.Header.Timestamp

	oldTarget := bc.Target
	bc.Target = block.AdjustDifficulty(oldTarget, actualTimeSpan)
	bc.TargetHex = block.BitsFromTarget(bc.Target)

	log.Printf("[DIFFICULTY] Adjusted at height %d — time span: %ds, old target: %s..., new target: %s...",
		height, actualTimeSpan, block.BitsFromTarget(oldTarget)[:16], bc.TargetHex[:16])
}

// GetTarget returns the current difficulty target.
func (bc *Blockchain) GetTarget() *big.Int {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return new(big.Int).Set(bc.Target)
}

// GetBlockReward returns the mining reward for the next block.
func (bc *Blockchain) GetBlockReward() float64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	nextHeight := bc.Height() + 1
	return block.BlockReward(nextHeight, bc.TotalMined)
}

// AllTransactions returns all transactions across all blocks (for compatibility).
func (bc *Blockchain) AllTransactions() []block.Transaction {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	var all []block.Transaction
	for _, b := range bc.Blocks {
		all = append(all, b.Transactions...)
	}
	return all
}

// ──────────────────────────────────────────────────────────────────────────────
// Serialization
// ──────────────────────────────────────────────────────────────────────────────

// ToJSON serializes the blockchain to indented JSON bytes.
func (bc *Blockchain) ToJSON() ([]byte, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return json.MarshalIndent(bc, "", "  ")
}

// FromJSON deserializes a blockchain from JSON bytes and rebuilds the target.
func FromJSON(data []byte) (*Blockchain, error) {
	var bc Blockchain
	if err := json.Unmarshal(data, &bc); err != nil {
		return nil, fmt.Errorf("chain deserialization failed: %w", err)
	}

	// Rebuild target from hex.
	if bc.TargetHex != "" {
		bc.Target = block.TargetFromBits(bc.TargetHex)
	} else {
		bc.Target = new(big.Int).Set(block.InitialTarget)
		bc.TargetHex = block.BitsFromTarget(block.InitialTarget)
	}

	return &bc, nil
}

// ValidateChain checks the full integrity of a blockchain from genesis to tip.
func ValidateChain(bc *Blockchain) error {
	if len(bc.Blocks) == 0 {
		return fmt.Errorf("empty blockchain")
	}

	for i, b := range bc.Blocks {
		var expectedPrevHash string
		if i == 0 {
			expectedPrevHash = "0000000000000000000000000000000000000000000000000000000000000000"
		} else {
			expectedPrevHash = bc.Blocks[i-1].Hash
		}

		if err := block.ValidateBlock(b, expectedPrevHash, uint64(i)); err != nil {
			return fmt.Errorf("block %d: %w", i, err)
		}
	}

	return nil
}
