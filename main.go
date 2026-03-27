// Noda — a Bitcoin-like cryptocurrency node in Go.
//
// Features:
//   - Block-based chain with Proof of Work (SHA-256 double-hash)
//   - Merkle Tree for transaction inclusion proofs
//   - Dynamic difficulty adjustment (every 2016 blocks)
//   - Block reward with halving (50 coins, halving every 210,000 blocks)
//   - UTXO set for balance tracking and double-spend prevention
//   - Mempool for unconfirmed transaction management
//   - Faucet: 5,000 coins per request, global cap 11,000,000 (no per-address cooldown)
//   - Mining rewards up to 10,000,000 (total supply cap: 21,000,000)
//   - Ed25519 cryptography for transaction signing
//   - HTTP API for wallet interactions
//   - Bitcoin-style TCP P2P protocol with binary message framing
//   - P2P networking with chain synchronization
//
// Configuration is read from environment variables first, then CLI flags.
// Environment variables take precedence over defaults but CLI flags override everything.
//
// Environment variables:
//
//	PORT       — HTTP port to listen on              (default: 3000)
//	P2P_PORT   — TCP P2P port to listen on           (default: 9333)
//	DATA_FILE  — path to the JSON storage file       (default: node_data.json)
//	FAUCET_KEY — hex-encoded Ed25519 private key     (optional)
//	PEERS      — comma-separated list of HTTP peer URLs  (optional)
//	TCP_PEERS  — comma-separated list of TCP peer addresses (host:port) (optional)
//
// CLI flags (override env vars):
//
//	-port        HTTP port to listen on
//	-p2p-port    TCP P2P port to listen on
//	-peers       Comma-separated list of HTTP peer URLs
//	-tcp-peers   Comma-separated list of TCP peer addresses (host:port)
//	-data        Path to the JSON storage file
//	-faucet-key  Hex-encoded Ed25519 private key for the faucet wallet
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Bihan293/Noda/api"
	"github.com/Bihan293/Noda/block"
	"github.com/Bihan293/Noda/ledger"
	"github.com/Bihan293/Noda/network"
	"github.com/Bihan293/Noda/p2p"
)

// envOrDefault returns the value of the environment variable named by key,
// or fallback if the variable is not set or empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	// ---- Defaults from environment variables ----
	defaultPort := envOrDefault("PORT", "3000")
	defaultP2PPort := envOrDefault("P2P_PORT", "9333")
	defaultData := envOrDefault("DATA_FILE", "node_data.json")
	defaultFaucet := envOrDefault("FAUCET_KEY", "")
	defaultPeers := envOrDefault("PEERS", "")
	defaultTCPPeers := envOrDefault("TCP_PEERS", "")

	// ---- CLI Flags (override env vars) ----
	port := flag.String("port", defaultPort, "HTTP port for this node (env: PORT)")
	p2pPort := flag.String("p2p-port", defaultP2PPort, "TCP P2P port for this node (env: P2P_PORT)")
	peersFlag := flag.String("peers", defaultPeers, "Comma-separated HTTP peer URLs (env: PEERS)")
	tcpPeersFlag := flag.String("tcp-peers", defaultTCPPeers, "Comma-separated TCP peer addresses host:port (env: TCP_PEERS)")
	dataFile := flag.String("data", defaultData, "Path to JSON storage file (env: DATA_FILE)")
	faucetKey := flag.String("faucet-key", defaultFaucet, "Hex-encoded Ed25519 private key for the faucet wallet (env: FAUCET_KEY)")
	flag.Parse()

	// ---- Parse HTTP peers ----
	var httpPeers []string
	if *peersFlag != "" {
		for _, p := range strings.Split(*peersFlag, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				httpPeers = append(httpPeers, p)
			}
		}
	}

	// ---- Parse TCP peers ----
	var tcpPeers []string
	if *tcpPeersFlag != "" {
		for _, p := range strings.Split(*tcpPeersFlag, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				tcpPeers = append(tcpPeers, p)
			}
		}
	}

	// ---- Initialize components ----
	log.Println("╔══════════════════════════════════════════════════════════════╗")
	log.Println("║          Noda Crypto Node — Bitcoin-like                     ║")
	log.Println("║   UTXO + Mempool + Faucet (11M cap) + TCP P2P              ║")
	log.Println("╚══════════════════════════════════════════════════════════════╝")
	log.Printf("  HTTP Port:     %s", *port)
	log.Printf("  P2P Port:      %s", *p2pPort)
	log.Printf("  Data:          %s", *dataFile)
	log.Printf("  HTTP Peers:    %v", httpPeers)
	log.Printf("  TCP Peers:     %v", tcpPeers)

	// Load or create ledger (chain + UTXO + mempool).
	l := ledger.LoadLedger(*dataFile)
	log.Printf("  Chain:         %d blocks (height: %d)", l.GetChain().Len(), l.GetChainHeight())
	log.Printf("  UTXO Set:      %d unspent outputs", l.UTXOSet.Size())
	log.Printf("  Mempool:       %d pending transactions", l.GetMempoolSize())
	log.Printf("  Block Reward:  %.2f coins", l.GetBlockReward())
	log.Printf("  Max Supply:    %.0f coins (%.0f faucet + %.0f mining)",
		block.MaxTotalSupply, block.GenesisSupply, block.MaxMiningSupply)

	// Configure faucet wallet if key is provided.
	if *faucetKey != "" {
		if err := l.SetFaucetKey(*faucetKey); err != nil {
			log.Fatalf("Faucet key error: %v", err)
		}
		log.Printf("  Faucet:        %s (balance: %.2f, remaining: %.0f)",
			l.FaucetAddress(), l.GetBalance(l.FaucetAddress()), l.FaucetRemaining())
	} else {
		log.Println("  Faucet:        disabled (set FAUCET_KEY or use -faucet-key to enable)")
	}

	// Create the HTTP network layer with initial peers.
	net := network.NewNetwork(httpPeers)

	// ---- Start TCP P2P Node ----
	var p2pPortNum uint16
	fmt.Sscanf(*p2pPort, "%d", &p2pPortNum)

	p2pNode := p2p.NewNode(p2pPortNum, l, tcpPeers)
	if err := p2pNode.Start(); err != nil {
		log.Printf("[P2P] Warning: TCP P2P failed to start: %v", err)
	} else {
		// Link TCP node to the network layer.
		net.SetTCPNode(p2pNode)
		log.Printf("  P2P:           TCP listening on port %d", p2pPortNum)
	}

	// Attempt initial sync from HTTP peers.
	if len(httpPeers) > 0 {
		log.Println("[SYNC] Fetching chain from HTTP peers...")
		if net.SyncChain(l) {
			log.Printf("[SYNC] Chain updated from peers — height: %d, UTXO: %d",
				l.GetChainHeight(), l.UTXOSet.Size())
		} else {
			log.Println("[SYNC] Local chain is up to date")
		}
	}

	// ---- Start HTTP server ----
	server := &api.Server{
		Ledger:  l,
		Network: net,
		Port:    *port,
	}

	// Handle graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\n[SHUTDOWN] Signal received, shutting down...")
		p2pNode.Stop()
		log.Println("[SHUTDOWN] P2P node stopped")
		os.Exit(0)
	}()

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
