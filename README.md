# Noda — Minimal Cryptocurrency Node in Go

A lightweight crypto node that supports transactions, Ed25519 signing, peer-to-peer networking, and chain synchronization via the longest-chain rule.

## Features

- **Ed25519 cryptography** — key generation, transaction signing, and verification
- **Transaction chain** — SHA-256 linked transactions with genesis block (11,000,000 coin supply)
- **Balance ledger** — in-memory balance map rebuilt from chain, persisted to JSON
- **HTTP API** — RESTful endpoints for balances, transactions, signing, sending, and faucet
- **P2P networking** — peer discovery, transaction broadcast, and chain sync
- **Longest-chain consensus** — simple rule: the longest valid chain wins
- **JSON persistence** — chain and balances saved to disk after every transaction
- **Faucet** — configurable test-coin dispenser with per-address cooldown
- **Structured logging** — every transaction, rejection, and sync event is logged

## Quick Start

```bash
# Build
go build -o noda .

# Run node on port 3000 (default)
./noda

# Run with faucet enabled (generate a key first, then fund it)
./noda -port 3000 -faucet-key "<hex_private_key>"

# Run with custom port, peers, and data file
./noda -port 3001 -peers "http://localhost:3000" -data node1.json
```

### Run Multiple Nodes

```bash
# Terminal 1 — main node with faucet
./noda -port 3000 -data node0.json -faucet-key "<hex_private_key>"

# Terminal 2 — connects to node 1
./noda -port 3001 -peers "http://localhost:3000" -data node1.json

# Terminal 3 — connects to both
./noda -port 3002 -peers "http://localhost:3000,http://localhost:3001" -data node2.json
```

## CLI Flags

| Flag           | Default          | Description                                      |
|----------------|------------------|--------------------------------------------------|
| `-port`        | `3000`           | HTTP port for the node                           |
| `-peers`       | (none)           | Comma-separated peer URLs                        |
| `-data`        | `node_data.json` | Path to the JSON persistence file                |
| `-faucet-key`  | (none)           | Hex-encoded Ed25519 private key for faucet wallet|

## API Endpoints

| Method | Endpoint            | Description                                    |
|--------|---------------------|------------------------------------------------|
| GET    | `/balance?address=` | Get balance for an address                     |
| POST   | `/transaction`      | Submit a pre-signed transaction                |
| GET    | `/chain`            | Get the full transaction chain                 |
| POST   | `/sign`             | Sign a transaction (returns signature, no broadcast) |
| POST   | `/send`             | Sign + validate + add to chain + broadcast     |
| POST   | `/faucet`           | Get free test coins (50 coins, 60s cooldown)   |
| GET    | `/generate-keys`    | Generate a new Ed25519 key pair                |
| GET    | `/status`           | Node info (port, chain length, peers, faucet)  |
| GET    | `/peers`            | List known peers                               |
| POST   | `/peers`            | Add a new peer `{"peer": "http://..."}`        |
| POST   | `/sync`             | Trigger chain sync from peers                  |

## Complete Usage Walkthrough

### Step 1: Generate Keys

```bash
curl -s http://localhost:3000/generate-keys | jq
```

Response:
```json
{
  "address": "a1b2c3d4...",
  "public_key": "a1b2c3d4...",
  "private_key": "e5f6a7b8..."
}
```

### Step 2: Get Test Coins from Faucet

```bash
curl -s -X POST http://localhost:3000/faucet \
  -H "Content-Type: application/json" \
  -d '{"to": "a1b2c3d4..."}' | jq
```

Response:
```json
{
  "message": "50 coins sent from faucet",
  "to": "a1b2c3d4...",
  "amount": 50,
  "new_balance": 50
}
```

### Step 3: Send Coins (Easy Way — POST /send)

The `/send` endpoint handles signing, validation, and broadcasting in one call:

```bash
curl -s -X POST http://localhost:3000/send \
  -H "Content-Type: application/json" \
  -d '{
    "to": "recipient_address_here",
    "amount": 25,
    "private_key": "your_private_key_here"
  }' | jq
```

Note: `from` is optional — it's derived from `private_key` automatically.

Response:
```json
{
  "message": "transaction sent",
  "id": "abc123...",
  "from": "a1b2c3d4...",
  "to": "recipient_address",
  "amount": 25,
  "signature": "def456...",
  "new_balance": 25
}
```

### Step 4: Sign Without Sending (POST /sign)

```bash
curl -s -X POST http://localhost:3000/sign \
  -H "Content-Type: application/json" \
  -d '{
    "from": "a1b2c3d4...",
    "to": "recipient_address",
    "amount": 10,
    "private_key": "your_private_key"
  }' | jq
```

Response:
```json
{
  "transaction": {
    "from": "a1b2c3d4...",
    "to": "recipient_address",
    "amount": 10,
    "signature": "hex_signature..."
  },
  "signature": "hex_signature..."
}
```

### Step 5: Submit a Pre-Signed Transaction

```bash
curl -s -X POST http://localhost:3000/transaction \
  -H "Content-Type: application/json" \
  -d '{
    "from": "a1b2c3d4...",
    "to": "recipient_address",
    "amount": 10,
    "signature": "hex_signature_from_sign_endpoint"
  }' | jq
```

### Step 6: Check Balance

```bash
curl -s "http://localhost:3000/balance?address=a1b2c3d4..." | jq
```

### Step 7: View Full Chain

```bash
curl -s http://localhost:3000/chain | jq
```

### Step 8: Node Status

```bash
curl -s http://localhost:3000/status | jq
```

Response:
```json
{
  "port": "3000",
  "chain_length": 5,
  "peers": 2,
  "faucet_address": "faucet_addr...",
  "faucet_balance": 10999800
}
```

### Peer Management

```bash
# List peers
curl -s http://localhost:3000/peers | jq

# Add a peer
curl -s -X POST http://localhost:3000/peers \
  -H "Content-Type: application/json" \
  -d '{"peer": "http://localhost:3001"}' | jq

# Sync chain from peers
curl -s -X POST http://localhost:3000/sync | jq
```

## Project Structure

```
.
├── main.go              # Entry point — CLI flags, component wiring
├── crypto/
│   └── crypto.go        # Ed25519 key gen, signing, verification, SignTransaction
├── chain/
│   └── chain.go         # Transaction struct, blockchain, hashing, genesis
├── ledger/
│   └── ledger.go        # Balances, validation, faucet, JSON persistence
├── api/
│   └── server.go        # HTTP server, all endpoints, request logging
├── network/
│   └── network.go       # Peer management, broadcast, chain sync
├── go.mod               # Go module (zero external dependencies)
└── README.md
```

## Validation Rules

1. **No coin creation** — only the genesis transaction mints coins
2. **Signature required** — every transaction must have a valid Ed25519 signature over `from:to:amount`
3. **Sufficient balance** — sender must have enough coins
4. **Positive amount** — transaction amount must be > 0
5. **No self-sends** — sender and receiver must differ
6. **Faucet cooldown** — 60-second cooldown per address

## Error Messages

The API returns clear, descriptive JSON error messages:

```json
{"error": "invalid signature: Ed25519 verification failed for sender a1b2c3d4..."}
{"error": "insufficient balance: address a1b2c3d4... has 25.000000 coins, tried to send 100.000000"}
{"error": "invalid amount: must be positive, got -5.000000"}
{"error": "faucet cooldown: try again in 45 seconds"}
{"error": "faucet not configured: start node with -faucet-key flag"}
```

## Logging

All important events are logged to stderr with structured prefixes:

```
[TX ACCEPTED]  a1b2c3d4...e5f6 -> 9a8b7c6d...1234 : 50.00 coins (chain length: 3)
[TX REJECTED]  invalid signature from a1b2c3d4...e5f6 -> ...
[TX REJECTED]  insufficient balance: a1b2c3d4... has 25.00, needs 100.00
[FAUCET]       Sent 50.00 coins to a1b2c3d4...e5f6
[FAUCET REJECTED] cooldown active for a1b2c3d4...e5f6 (45s remaining)
[SYNC]         Chain replaced — new length: 12
[HTTP]         POST /send — 1.234ms
```

## Design Decisions

- **Ed25519** over ECDSA: faster, deterministic signatures, no nonce pitfalls
- **No mining/blocks**: transactions are directly chained — simpler model
- **Longest-chain rule**: the node with the most transactions wins during sync
- **JSON storage**: human-readable, easy to debug; fine for a lightweight node
- **No external dependencies**: uses only the Go standard library
- **Faucet**: simplifies testing — no need to manually sign genesis transactions

## License

MIT
