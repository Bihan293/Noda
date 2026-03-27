// Noda — a minimal cryptocurrency node in Go.
//
// Configuration is read from environment variables first, then CLI flags.
// Environment variables take precedence over defaults but CLI flags override everything.
//
// Environment variables:
//
//	PORT       — HTTP port to listen on              (default: 3000)
//	DATA_FILE  — path to the JSON storage file       (default: node_data.json)
//	FAUCET_KEY — hex-encoded Ed25519 private key     (optional)
//	PEERS      — comma-separated list of peer URLs   (optional)
//
// CLI flags (override env vars):
//
//	-port        HTTP port to listen on
//	-peers       Comma-separated list of peer URLs
//	-data        Path to the JSON storage file
//	-faucet-key  Hex-encoded Ed25519 private key for the faucet wallet
//
// Usage:
//
//	# Using environment variables (Docker, Render, etc.)
//	PORT=8080 PEERS="http://node1:3000" ./noda
//
//	# Using CLI flags (local development)
//	./noda -port 3001 -peers "http://localhost:3000" -data node1.json
package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/Bihan293/Noda/api"
	"github.com/Bihan293/Noda/ledger"
	"github.com/Bihan293/Noda/network"
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
	defaultData := envOrDefault("DATA_FILE", "node_data.json")
	defaultFaucet := envOrDefault("FAUCET_KEY", "")
	defaultPeers := envOrDefault("PEERS", "")

	// ---- CLI Flags (override env vars) ----
	port := flag.String("port", defaultPort, "HTTP port for this node (env: PORT)")
	peersFlag := flag.String("peers", defaultPeers, "Comma-separated peer URLs (env: PEERS)")
	dataFile := flag.String("data", defaultData, "Path to JSON storage file (env: DATA_FILE)")
	faucetKey := flag.String("faucet-key", defaultFaucet, "Hex-encoded Ed25519 private key for the faucet wallet (env: FAUCET_KEY)")
	flag.Parse()

	// ---- Parse peers ----
	var peers []string
	if *peersFlag != "" {
		for _, p := range strings.Split(*peersFlag, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				peers = append(peers, p)
			}
		}
	}

	// ---- Initialize components ----
	log.Println("╔══════════════════════════════════════╗")
	log.Println("║         Noda Crypto Node             ║")
	log.Println("╚══════════════════════════════════════╝")
	log.Printf("  Port:    %s", *port)
	log.Printf("  Data:    %s", *dataFile)
	log.Printf("  Peers:   %v", peers)

	// Load or create ledger (chain + balances).
	l := ledger.LoadLedger(*dataFile)
	log.Printf("  Chain:   %d transactions loaded", l.GetChain().Len())

	// Configure faucet wallet if key is provided.
	if *faucetKey != "" {
		if err := l.SetFaucetKey(*faucetKey); err != nil {
			log.Fatalf("Faucet key error: %v", err)
		}
		log.Printf("  Faucet:  %s (balance: %.2f)", l.FaucetAddress(), l.GetBalance(l.FaucetAddress()))
	} else {
		log.Println("  Faucet:  disabled (set FAUCET_KEY or use -faucet-key to enable)")
	}

	// Create the network layer with initial peers.
	net := network.NewNetwork(peers)

	// Attempt initial sync from peers.
	if len(peers) > 0 {
		log.Println("[SYNC] Fetching chain from peers...")
		if net.SyncChain(l) {
			log.Printf("[SYNC] Chain updated from peers — length: %d", l.GetChain().Len())
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

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
