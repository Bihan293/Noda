# Noda — Minimal Cryptocurrency Node in Go

A lightweight Bitcoin-like crypto node with PoW mining, UTXO model, TCP P2P protocol, Prometheus metrics, and a 21M supply cap.

## Features

- **Ed25519 cryptography** — key generation, transaction signing, and verification
- **Block-based chain** — SHA-256 double-hash PoW with Merkle trees
- **Dynamic difficulty** — adjusts every 2,016 blocks (target: ~10 min/block)
- **Block reward halving** — 50 coins initial, halving every 210,000 blocks
- **UTXO model** — unspent transaction output tracking for balance integrity
- **Mempool** — in-memory unconfirmed transaction pool with eviction
- **Faucet** — 5,000 coins per request, global 11M cap, permanently disabled after cap
- **Tokenomics** — 21M total supply (11M faucet + 10M mining)
- **HTTP API** — RESTful endpoints with rate limiting and security headers
- **TCP P2P** — Bitcoin-style binary protocol with handshake, inventory, block/tx relay
- **Prometheus metrics** — `/metrics` endpoint for monitoring
- **Structured logging** — `log/slog` with configurable levels
- **Graceful shutdown** — context-based with OS signal handling
- **Rate limiting** — per-IP token bucket limiter
- **Docker-ready** — multi-stage build + Docker Compose for multi-node networks
- **Zero dependencies** — uses only the Go standard library

---

## Configuration

Noda reads configuration from **environment variables** first, then **CLI flags**.
CLI flags override environment variables. Both fall back to sensible defaults.

| Env Variable | CLI Flag       | Default          | Description                                      |
|--------------|----------------|------------------|--------------------------------------------------|
| `PORT`       | `-port`        | `3000`           | HTTP port for the node                           |
| `P2P_PORT`   | `-p2p-port`    | `9333`           | TCP P2P port for peer connections                |
| `DATA_FILE`  | `-data`        | `node_data.json` | Path to the JSON persistence file                |
| `FAUCET_KEY` | `-faucet-key`  | (none)           | Hex-encoded Ed25519 private key for faucet wallet|
| `PEERS`      | `-peers`       | (none)           | Comma-separated HTTP peer URLs                   |
| `TCP_PEERS`  | `-tcp-peers`   | (none)           | Comma-separated TCP peer addresses (host:port)   |
| `LOG_LEVEL`  | `-log-level`   | `info`           | Log level: debug, info, warn, error              |
| `RATE_LIMIT` | `-rate-limit`  | `10`             | Max requests per second per IP                   |

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
PORT=8080 DATA_FILE=mydata.json LOG_LEVEL=debug ./noda

# Run with CLI flags
./noda -port 3001 -peers "http://localhost:3000" -data node1.json

# Run with faucet enabled
FAUCET_KEY="your_hex_private_key" ./noda

# Run with rate limiting adjusted
RATE_LIMIT=50 ./noda
```

### Run multiple nodes locally

```bash
# Terminal 1 — main node with faucet
FAUCET_KEY="<hex_private_key>" ./noda

# Terminal 2 — connects to node 1
PORT=3001 P2P_PORT=9334 PEERS="http://localhost:3000" TCP_PEERS="localhost:9333" DATA_FILE=node1.json ./noda

# Terminal 3 — connects to both
PORT=3002 P2P_PORT=9335 PEERS="http://localhost:3000,http://localhost:3001" TCP_PEERS="localhost:9333,localhost:9334" DATA_FILE=node2.json ./noda
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
docker run -p 3000:3000 -p 9333:9333 noda

# With persistent data
docker run -p 3000:3000 -v noda-data:/app noda

# With faucet enabled
docker run -p 3000:3000 \
  -e FAUCET_KEY="your_hex_private_key" \
  noda

# Full example
docker run -d --name noda-node \
  -p 3000:3000 -p 9333:9333 \
  -e PORT=3000 \
  -e FAUCET_KEY="your_hex_private_key" \
  -e LOG_LEVEL=info \
  -e RATE_LIMIT=20 \
  -v noda-data:/app \
  noda
```

### Run a multi-node network with Docker Compose

```bash
# Start all 3 nodes
docker compose up --build

# Stop
docker compose down

# View logs
docker compose logs -f
```

Nodes:
- **Node 1**: http://localhost:3000 (faucet-capable)
- **Node 2**: http://localhost:3001
- **Node 3**: http://localhost:3002

---

## API Endpoints

| Method | Endpoint            | Description                                    |
|--------|---------------------|------------------------------------------------|
| GET    | `/health`           | Lightweight health check                       |
| GET    | `/metrics`          | Prometheus-format metrics                      |
| GET    | `/balance?address=` | Get balance for an address                     |
| POST   | `/transaction`      | Submit a pre-signed transaction                |
| GET    | `/chain`            | Get the full blockchain                        |
| POST   | `/sign`             | Sign a transaction (returns signature, no broadcast) |
| POST   | `/send`             | Sign + validate + add to chain + broadcast     |
| POST   | `/faucet`           | Get free coins (5,000 per request, 11M cap)    |
| GET    | `/generate-keys`    | Generate a new Ed25519 key pair                |
| GET    | `/status`           | Node info (height, peers, faucet, UTXO, mining)|
| GET    | `/mempool`          | View pending transactions                      |
| GET    | `/peers`            | List known peers                               |
| POST   | `/peers`            | Add a new peer `{"peer": "http://..."}`        |
| POST   | `/sync`             | Trigger chain sync from peers                  |

## Complete Usage Walkthrough

### Step 1: Generate Keys

```bash
curl -s http://localhost:3000/generate-keys | jq
```

### Step 2: Get Test Coins from Faucet

```bash
curl -s -X POST http://localhost:3000/faucet \
  -H "Content-Type: application/json" \
  -d '{"to": "your_address_here"}' | jq
```

### Step 3: Send Coins

```bash
curl -s -X POST http://localhost:3000/send \
  -H "Content-Type: application/json" \
  -d '{
    "to": "recipient_address_here",
    "amount": 25,
    "private_key": "your_private_key_here"
  }' | jq
```

### Step 4: Check Balance

```bash
curl -s "http://localhost:3000/balance?address=your_address" | jq
```

### Step 5: View Metrics

```bash
curl -s http://localhost:3000/metrics
```

### Step 6: Health Check

```bash
curl -s http://localhost:3000/health | jq
```

---

## Monitoring

### Prometheus Metrics

The `/metrics` endpoint exposes metrics in Prometheus text exposition format:

```
noda_block_height 42
noda_block_count 43
noda_total_mined_coins 2100
noda_total_faucet_coins 50000
noda_block_reward 50
noda_mempool_size 3
noda_utxo_count 128
noda_peer_count_total 5
noda_faucet_remaining_coins 1.095e+07
noda_faucet_active 1
noda_http_requests_total 1234
noda_tx_accepted_total 42
noda_tx_rejected_total 7
noda_blocks_mined_total 42
```

### Grafana Dashboard

You can import these metrics into Grafana for visualization. Configure Prometheus to scrape `http://your-node:3000/metrics`.

---

## Project Structure

```
.
├── main.go                  # Entry point — config, slog, graceful shutdown, component wiring
├── Dockerfile               # Multi-stage Docker build (golang:alpine → alpine)
├── docker-compose.yml       # Multi-node local network (3 nodes)
├── .dockerignore            # Excludes unnecessary files from Docker context
├── crypto/
│   ├── crypto.go            # Ed25519 key gen, signing, verification
│   └── crypto_test.go       # Key pair, sign/verify, address derivation tests
├── block/
│   ├── block.go             # Block structure, PoW, Merkle tree, halving, genesis
│   └── block_test.go        # PoW, difficulty, halving, Merkle, validation tests
├── chain/
│   ├── chain.go             # Blockchain management and serialization
│   └── chain_test.go        # Chain creation, block addition, JSON round-trip tests
├── mempool/
│   ├── mempool.go           # In-memory unconfirmed transaction pool
│   └── mempool_test.go      # Add/remove, FIFO, eviction, double-spend tests
├── utxo/
│   ├── utxo.go              # Unspent Transaction Output set
│   └── utxo_test.go         # Add/spend, balance, ApplyBlock, rebuild tests
├── ledger/
│   ├── ledger.go            # Ledger: chain + UTXO + mempool + faucet
│   └── ledger_test.go       # Faucet, validation, persistence, replacement tests
├── api/
│   ├── server.go            # HTTP server with rate limiting, metrics, security
│   └── server_test.go       # All endpoint tests with httptest
├── network/
│   ├── network.go           # HTTP-based P2P networking
│   └── network_test.go      # Peer management tests
├── p2p/
│   ├── message.go           # TCP protocol messages and wire encoding
│   ├── node.go              # P2P node, handshake, block/tx relay
│   └── p2p_test.go          # Message encoding, peer state, payload tests
├── metrics/
│   └── metrics.go           # Prometheus-compatible metrics (zero deps)
├── ratelimit/
│   └── ratelimit.go         # Per-IP token bucket rate limiter
├── integration/
│   └── integration_test.go  # End-to-end tests (mining, UTXO, tokenomics)
├── .github/
│   └── workflows/
│       └── ci.yml           # GitHub Actions CI (build, test, vet, docker)
├── go.mod                   # Go module (zero external dependencies)
├── CONTRIBUTING.md          # Contribution guidelines
├── CHANGELOG.md             # Version history
├── ROADMAP.md               # Development roadmap
└── README.md
```

## Tokenomics

```
Genesis Supply:           11,000,000 coins (minted at genesis)
Faucet Distribution:      5,000 coins per request
  - Any address can claim (multiple times)
  - Global cap: 11,000,000 total coins via faucet
  - Once 11M distributed → faucet permanently disabled
Mining Rewards:           Starts at 50 coins/block
  - Halving every 210,000 blocks
  - Mining continues until total supply reaches 21,000,000
Max Total Supply:         21,000,000 coins
  - 11,000,000 from faucet (genesis)
  - 10,000,000 from mining rewards
Difficulty Adjustment:    Every 2,016 blocks (target: ~10 min/block)
```

## Testing

Noda has comprehensive test coverage across all packages.

### Run all tests

```bash
go test ./... -v -race -count=1
```

### Run tests for a specific package

```bash
go test ./block/ -v
go test ./crypto/ -v
go test ./integration/ -v
```

### Test Coverage

| Package | Tests |
|---------|-------|
| `crypto/` | Key generation, sign/verify round-trip, invalid inputs |
| `block/` | PoW mining, Merkle tree, difficulty adjustment, halving, genesis, validation |
| `chain/` | Blockchain creation, block addition, serialization, chain validation |
| `mempool/` | Add/remove, FIFO ordering, eviction, double-spend detection |
| `utxo/` | Add/spend, balance queries, ApplyBlock, rebuild from blocks |
| `ledger/` | Faucet state, transaction validation, persistence, chain replacement |
| `p2p/` | Message encoding/decoding, peer state, payload round-trips |
| `network/` | Peer management, broadcast |
| `api/` | All HTTP endpoints, error responses, middleware |
| `integration/` | End-to-end mining, UTXO consistency, tokenomics verification |

### CI Pipeline

GitHub Actions runs on every push and pull request:
- `go build ./...` — compilation check
- `go test ./... -v -race` — full test suite with race detector
- `go vet ./...` — static analysis
- `gofmt` — formatting check
- Docker build + health check

## Security

- **Rate limiting**: Configurable per-IP token bucket (default: 10 req/s)
- **Input validation**: Hex address format, length limits
- **Body size limits**: 64 KB max request body
- **Server timeouts**: Read (15s), Write (30s), Idle (60s)
- **Security headers**: X-Content-Type-Options, X-Frame-Options, X-XSS-Protection
- **Peer banning**: Misbehaving peers are banned for 24 hours
- **Non-root Docker**: Runs as unprivileged `noda` user

## Design Decisions

- **Ed25519** over ECDSA: faster, deterministic signatures, no nonce pitfalls
- **log/slog** for structured logging: key-value pairs, configurable levels, machine-parseable
- **Custom Prometheus metrics**: zero external dependencies, standard text format
- **Token bucket rate limiter**: smooth rate control, burst tolerance
- **Graceful shutdown**: context cancellation, connection draining, 10s timeout
- **UTXO model**: prevents double-spend, enables parallel validation
- **Longest-chain rule**: the node with the most blocks wins during sync
- **JSON storage**: human-readable, easy to debug; fine for a lightweight node
- **No external dependencies**: uses only the Go standard library
- **Multi-stage Docker build**: ~15 MB final image, non-root user, health check included

## License

MIT
