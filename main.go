// Noda — a minimal cryptocurrency node in Go.
//
// Usage:
//
//	go run main.go -port 3000 -peers "http://localhost:3001,http://localhost:3002"
//	go run main.go -port 3001 -data node1.json
//
// Flags:
//
//	-port   HTTP port to listen on  (default: 3000)
//	-peers  Comma-separated list of peer URLs
//	-data   Path to the JSON storage file  (default: node_data.json)
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
	log.Printf("=== Noda Crypto Node ===")
	log.Printf("Port:    %s", *port)
	log.Printf("Data:    %s", *dataFile)
	log.Printf("Peers:   %v", peers)

	// Load or create ledger (chain + balances).
	l := ledger.LoadLedger(*dataFile)
	log.Printf("Chain loaded: %d transactions", l.GetChain().Len())

	// Create the network layer with initial peers.
	net := network.NewNetwork(peers)

	// Attempt initial sync from peers.
	if len(peers) > 0 {
		log.Println("Syncing chain from peers...")
		if net.SyncChain(l) {
			log.Println("Chain updated from peers")
		} else {
			log.Println("Local chain is up to date")
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
