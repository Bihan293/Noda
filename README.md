# Noda — Minimal Cryptocurrency Node in Go

A lightweight crypto node that supports transactions, Ed25519 signing, peer-to-peer networking, and chain synchronization via the longest-chain rule.

## Features

- **Ed25519 cryptography** — key generation, transaction signing, and verification
- **Transaction chain** — SHA-256 linked transactions with genesis block (11,000,000 coin supply)
- **Balance ledger** — in-memory balance map rebuilt from chain, persisted to JSON
- **HTTP API** — RESTful endpoints for balances, transactions, chain inspection, and key generation
- **P2P networking** — peer discovery, transaction broadcast, and chain sync
- **Longest-chain consensus** — simple rule: the longest valid chain wins
- **JSON persistence** — chain and balances saved to disk after every transaction

## Quick Start

```bash
# Build
go build -o noda .

# Run node on port 3000 (default)
./noda

# Run with custom port and peers
./noda -port 3001 -peers "http://localhost:3000" -data node1.json
```

### Run Multiple Nodes

```bash
# Terminal 1
./noda -port 3000 -data node0.json

# Terminal 2
./noda -port 3001 -peers "http://localhost:3000" -data node1.json

# Terminal 3
./noda -port 3002 -peers "http://localhost:3000,http://localhost:3001" -data node2.json
```

## API Endpoints

| Method | Endpoint         | Description                        |
|--------|------------------|------------------------------------|
| GET    | `/balance?address=` | Get balance for an address      |
| POST   | `/transaction`   | Submit a signed transaction        |
| GET    | `/chain`         | Get the full transaction chain     |
| GET    | `/generate-keys` | Generate a new Ed25519 key pair    |
| GET    | `/status`        | Node info (port, chain length, peers) |
| GET    | `/peers`         | List known peers                   |
| POST   | `/peers`         | Add a new peer `{"peer": "http://..."}` |
| POST   | `/sync`          | Trigger chain sync from peers      |

## Usage Example

### 1. Generate Keys

```bash
curl http://localhost:3000/generate-keys
```

Response:
```json
{
  "address": "a1b2c3...",
  "public_key": "a1b2c3...",
  "private_key": "d4e5f6..."
}
```

### 2. Send a Transaction

Sign the message `from:to:amount` with the sender's private key, then:

```bash
curl -X POST http://localhost:3000/transaction \
  -H "Content-Type: application/json" \
  -d '{
    "from": "0000000000000000000000000000000000000000000000000000000000000000",
    "to": "a1b2c3...",
    "amount": 100,
    "signature": "signed_hex..."
  }'
```

### 3. Check Balance

```bash
curl "http://localhost:3000/balance?address=a1b2c3..."
```

### 4. View Chain

```bash
curl http://localhost:3000/chain
```

### 5. Add a Peer

```bash
curl -X POST http://localhost:3000/peers \
  -H "Content-Type: application/json" \
  -d '{"peer": "http://localhost:3001"}'
```

### 6. Sync Chain

```bash
curl -X POST http://localhost:3000/sync
```

## Project Structure

```
.
├── main.go              # Entry point — CLI flags, component wiring
├── crypto/
│   └── crypto.go        # Ed25519 key generation, signing, verification
├── chain/
│   └── chain.go         # Transaction struct, blockchain, hashing, genesis
├── ledger/
│   └── ledger.go        # Balance management, validation, JSON persistence
├── api/
│   └── server.go        # HTTP server and all endpoint handlers
├── network/
│   └── network.go       # Peer management, broadcast, chain sync
├── go.mod               # Go module definition
└── README.md
```

## Design Decisions

- **Ed25519** over ECDSA: faster, deterministic signatures, no nonce pitfalls
- **No mining/blocks**: transactions are directly chained — simpler model
- **Longest-chain rule**: the node with the most transactions wins during sync
- **JSON storage**: human-readable, easy to debug; fine for a lightweight node
- **No external dependencies**: uses only the Go standard library

## CLI Flags

| Flag     | Default          | Description                           |
|----------|------------------|---------------------------------------|
| `-port`  | `3000`           | HTTP port for the node                |
| `-peers` | (none)           | Comma-separated peer URLs             |
| `-data`  | `node_data.json` | Path to the JSON persistence file     |

## License

MIT
