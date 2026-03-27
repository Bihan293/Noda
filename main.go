// Noda — a minimal cryptocurrency node in Go.
//
// Usage:
//
//	go run main.go -port 3000 -peers "http://localhost:3001,http://localhost:3002"
//	go run main.go -port 3001 -data node1.json -faucet-key "hex_private_key"
//
// Flags:
//
//	-port        HTTP port to listen on  (default: 3000)
//	-peers       Comma-separated list of peer URLs
//	-data        Path to the JSON storage file  (default: node_data.json)
//	-faucet-key  Hex-encoded Ed25519 private key for the faucet wallet
package main

import (
	"flag"
	"log"
	"strings"

	"github.com/Bihan293/Noda/api"
	"github.com/Bihan293/Noda/ledger"
	"github.com/Bihan293/Noda/network"
)

func main() {
	// ---- CLI Flags ----
	port := flag.String("port", "3000", "HTTP port for this node")
	peersFlag := flag.String("peers", "", "Comma-separated peer URLs (e.g. http://localhost:3001)")
	dataFile := flag.String("data", "node_data.json", "Path to JSON storage file")
	faucetKey := flag.String("faucet-key", "", "Hex-encoded Ed25519 private key for the faucet wallet")
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
		log.Println("  Faucet:  disabled (use -faucet-key to enable)")
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
