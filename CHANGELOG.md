# Changelog

All notable changes to Noda are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Full test suite for all packages:
  - `crypto/` — key generation, sign/verify round-trip, error handling
  - `block/` — PoW, Merkle tree, difficulty adjustment, halving, genesis block, validation
  - `chain/` — blockchain creation, block addition, serialization, chain validation
  - `mempool/` — add/remove/evict, FIFO ordering, double-spend detection
  - `utxo/` — add/spend/balance, ApplyBlock, rebuild from blocks, serialization
  - `ledger/` — faucet state, transaction validation, persistence, chain replacement
  - `p2p/` — message encoding/decoding, peer state, payload round-trips
  - `network/` — peer management, broadcast
  - `api/` — all HTTP endpoints, error responses, middleware
  - `integration/` — end-to-end mining, UTXO consistency, tokenomics verification
- CI pipeline via GitHub Actions (build, test, vet)
- `CONTRIBUTING.md` — contribution guidelines
- `CHANGELOG.md` — this file

## [0.4.0] — 2026-03-27

### Added
- Bitcoin-style TCP P2P protocol (`p2p/` package)
  - Binary message framing (magic + command + payload)
  - Version/verack handshake
  - Ping/pong keep-alive
  - Inventory system (inv, getdata)
  - Block and transaction relay
  - Initial Block Download (IBD) via getblocks
  - Peer discovery via addr messages
  - Ban system for misbehaving peers

## [0.3.0] — 2026-03-27

### Added
- Mempool (`mempool/` package) — in-memory pool of unconfirmed transactions
- UTXO set (`utxo/` package) — unspent transaction output tracking
- Faucet global cap enforcement: 5,000 coins/request, total cap 11,000,000
- Per-address cooldown removed; replaced by global cap logic

## [0.2.0] — 2026-03-27

### Added
- Block structure with Header and Body (`block/` package)
- Proof of Work mining (SHA-256 double-hash)
- Binary Merkle Tree for transaction inclusion proofs
- Dynamic difficulty adjustment (every 2,016 blocks)
- Block reward with halving (50 coins, halving every 210,000 blocks)
- Coinbase transaction generation
- Genesis block

## [0.1.0] — 2026-03-27

### Added
- Initial cryptocurrency node implementation
- Ed25519 cryptography for transaction signing
- Flat transaction chain with SHA-256 linking
- In-memory balance ledger with JSON persistence
- HTTP REST API for wallet interactions
- Basic HTTP-based P2P networking
- Faucet with per-address cooldown
- Docker support (multi-stage build)
