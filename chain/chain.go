// Package chain defines the transaction and blockchain data structures.
// Transactions are linked into a chain via sequential hashes, forming an
// append-only ledger similar to a simplified blockchain.
package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// GenesisSupply is the total fixed coin supply created in the genesis transaction.
const GenesisSupply = 11_000_000

// GenesisAddress is a well-known address that holds all coins at the start.
// It is the hex-encoded string of "GENESIS" padded to look like a public key.
const GenesisAddress = "0000000000000000000000000000000000000000000000000000000000000000"

// Transaction represents a single transfer of coins between two addresses.
type Transaction struct {
	ID        string  `json:"id"`        // SHA-256 hash of the transaction content
	From      string  `json:"from"`      // sender address (hex public key)
	To        string  `json:"to"`        // receiver address (hex public key)
	Amount    float64 `json:"amount"`    // coin amount (must be > 0)
	Timestamp int64   `json:"timestamp"` // unix timestamp (seconds)
	Signature string  `json:"signature"` // hex-encoded Ed25519 signature
	PrevHash  string  `json:"prev_hash"` // hash of the previous transaction in the chain
}

// Blockchain is an ordered list of transactions forming the ledger.
type Blockchain struct {
	Transactions []Transaction `json:"transactions"`
}

// NewBlockchain creates an empty blockchain and inserts the genesis transaction.
// The genesis TX creates the total supply and assigns it to GenesisAddress.
func NewBlockchain() *Blockchain {
	bc := &Blockchain{
		Transactions: make([]Transaction, 0),
	}
	bc.addGenesis()
	return bc
}

// addGenesis appends the genesis transaction that mints the entire coin supply.
// It has no "from" address, no signature, and an empty previous hash.
func (bc *Blockchain) addGenesis() {
	genesis := Transaction{
		From:      "",               // no sender — coins are created
		To:        GenesisAddress,
		Amount:    GenesisSupply,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
		Signature: "genesis",
		PrevHash:  "",
	}
	genesis.ID = HashTransaction(genesis)
	bc.Transactions = append(bc.Transactions, genesis)
}

// LastHash returns the hash of the most recent transaction in the chain.
func (bc *Blockchain) LastHash() string {
	if len(bc.Transactions) == 0 {
		return ""
	}
	return bc.Transactions[len(bc.Transactions)-1].ID
}

// AddTransaction appends a validated transaction to the chain.
// The caller is responsible for validation before calling this.
func (bc *Blockchain) AddTransaction(tx Transaction) {
	tx.PrevHash = bc.LastHash()
	tx.ID = HashTransaction(tx)
	bc.Transactions = append(bc.Transactions, tx)
}

// Len returns the number of transactions in the chain.
func (bc *Blockchain) Len() int {
	return len(bc.Transactions)
}

// HashTransaction computes the SHA-256 hash of a transaction's content.
// The ID field is excluded from hashing to avoid circular dependency.
func HashTransaction(tx Transaction) string {
	record := fmt.Sprintf("%s:%s:%f:%d:%s:%s",
		tx.From, tx.To, tx.Amount, tx.Timestamp, tx.Signature, tx.PrevHash)
	h := sha256.Sum256([]byte(record))
	return hex.EncodeToString(h[:])
}

// ToJSON serializes the blockchain to indented JSON bytes.
func (bc *Blockchain) ToJSON() ([]byte, error) {
	return json.MarshalIndent(bc, "", "  ")
}

// FromJSON deserializes a blockchain from JSON bytes.
func FromJSON(data []byte) (*Blockchain, error) {
	var bc Blockchain
	if err := json.Unmarshal(data, &bc); err != nil {
		return nil, fmt.Errorf("chain deserialization failed: %w", err)
	}
	return &bc, nil
}
