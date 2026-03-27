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
- **Docker-ready** — multi-stage build, minimal image, deploy anywhere
- **Environment variables** — configure via env vars for cloud/container deployment

---

## Configuration

Noda reads configuration from **environment variables** first, then **CLI flags**.
CLI flags override environment variables. Both fall back to sensible defaults.

| Env Variable | CLI Flag       | Default          | Description                                      |
|--------------|----------------|------------------|--------------------------------------------------|
| `PORT`       | `-port`        | `3000`           | HTTP port for the node                           |
| `DATA_FILE`  | `-data`        | `node_data.json` | Path to the JSON persistence file                |
| `FAUCET_KEY` | `-faucet-key`  | (none)           | Hex-encoded Ed25519 private key for faucet wallet|
| `PEERS`      | `-peers`       | (none)           | Comma-separated peer URLs                        |

---

## How to Run Locally

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)

### Build and run

```bash
# Build the binary
go build -o noda .

# Run with defaults (port 3000, data in node_data.json)
./noda

# Run with environment variables
PORT=8080 DATA_FILE=mydata.json ./noda

# Run with CLI flags
./noda -port 3001 -peers "http://localhost:3000" -data node1.json

# Run with faucet enabled
FAUCET_KEY="your_hex_private_key" ./noda

# Or mix both
PORT=3001 ./noda -peers "http://localhost:3000"
```

### Run multiple nodes locally

```bash
# Terminal 1 — main node with faucet
FAUCET_KEY="<hex_private_key>" ./noda

# Terminal 2 — connects to node 1
PORT=3001 PEERS="http://localhost:3000" DATA_FILE=node1.json ./noda

# Terminal 3 — connects to both
PORT=3002 PEERS="http://localhost:3000,http://localhost:3001" DATA_FILE=node2.json ./noda
```

---

## How to Run with Docker

### Build the image

```bash
docker build -t noda .
```

### Run a single node

```bash
# Basic run (port 3000)
docker run -p 3000:3000 noda

# Custom port
docker run -e PORT=8080 -p 8080:8080 noda

# With persistent data (survives container restart)
docker run -p 3000:3000 -v noda-data:/app noda

# With faucet enabled
docker run -p 3000:3000 \
  -e FAUCET_KEY="your_hex_private_key" \
  noda

# With peers
docker run -p 3000:3000 \
  -e PEERS="http://host.docker.internal:3001,http://host.docker.internal:3002" \
  noda

# Full example — all options
docker run -d --name noda-node \
  -p 3000:3000 \
  -e PORT=3000 \
  -e DATA_FILE=/app/node_data.json \
  -e FAUCET_KEY="your_hex_private_key" \
  -e PEERS="http://peer1:3000,http://peer2:3000" \
  -v noda-data:/app \
  noda
```

### Run a multi-node network with Docker

```bash
# Create a network
docker network create noda-net

# Node 1 — main node with faucet
docker run -d --name node1 --network noda-net \
  -p 3000:3000 \
  -e FAUCET_KEY="your_hex_private_key" \
  noda

# Node 2 — peers with node 1
docker run -d --name node2 --network noda-net \
  -p 3001:3001 \
  -e PORT=3001 \
  -e PEERS="http://node1:3000" \
  noda

# Node 3 — peers with both
docker run -d --name node3 --network noda-net \
  -p 3002:3002 \
  -e PORT=3002 \
  -e PEERS="http://node1:3000,http://node2:3001" \
  noda
```

---

## How to Deploy to Render

[Render](https://render.com) supports Docker-based deployments natively.

### Option 1: Deploy via Render Dashboard

1. Push this repo to GitHub.
2. Go to [Render Dashboard](https://dashboard.render.com) → **New** → **Web Service**.
3. Connect your GitHub repository.
4. Render auto-detects the `Dockerfile` — no extra config needed.
5. Set environment variables in the Render dashboard:

   | Key          | Value                        |
   |--------------|------------------------------|
   | `PORT`       | `10000` (Render assigns this)|
   | `DATA_FILE`  | `/app/node_data.json`        |
   | `FAUCET_KEY` | `your_hex_private_key`       |
   | `PEERS`      | `https://other-node.onrender.com` |

6. Click **Deploy**.

> **Note:** Render automatically sets the `PORT` environment variable. Noda reads it automatically — no code changes needed.

### Option 2: Deploy with `render.yaml`

Create a `render.yaml` in the repo root:

```yaml
services:
  - type: web
    name: noda
    runtime: docker
    plan: free
    envVars:
      - key: DATA_FILE
        value: /app/node_data.json
      - key: FAUCET_KEY
        sync: false  # set manually in dashboard for security
```

Then connect the repo in Render and it will pick up the config.

---

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
├── main.go              # Entry point — env vars, CLI flags, component wiring
├── Dockerfile           # Multi-stage Docker build (golang:alpine → alpine)
├── .dockerignore        # Excludes unnecessary files from Docker context
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
- **Environment variables**: cloud-native config — works with Docker, Render, Railway, Fly.io, etc.
- **Multi-stage Docker build**: ~15 MB final image, non-root user, health check included

## License

MIT
