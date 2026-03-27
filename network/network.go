// Package network handles peer-to-peer communication between nodes.
// It maintains a list of known peers and provides methods to broadcast
// transactions and synchronize chains using the longest-chain rule.
package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Bihan293/Noda/chain"
	"github.com/Bihan293/Noda/ledger"
)

// httpTimeout is the maximum time we wait for peer responses.
const httpTimeout = 5 * time.Second

// Network manages peer connections and cross-node communication.
type Network struct {
	peers  []string     // list of peer base URLs, e.g. "http://localhost:3001"
	mu     sync.RWMutex // guards the peers slice
	client *http.Client // shared HTTP client with timeout
}

// NewNetwork creates a network manager with an optional initial set of peers.
func NewNetwork(initialPeers []string) *Network {
	peers := make([]string, 0, len(initialPeers))
	peers = append(peers, initialPeers...)
	return &Network{
		peers:  peers,
		client: &http.Client{Timeout: httpTimeout},
	}
}

// AddPeer adds a peer URL if it isn't already known.
func (n *Network) AddPeer(url string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range n.peers {
		if p == url {
			return // already known
		}
	}
	n.peers = append(n.peers, url)
	log.Printf("📡 Peer added: %s (total: %d)", url, len(n.peers))
}

// GetPeers returns a copy of the peer list.
func (n *Network) GetPeers() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	cp := make([]string, len(n.peers))
	copy(cp, n.peers)
	return cp
}

// BroadcastTransaction sends a transaction to all known peers via POST /transaction.
// Errors from individual peers are logged but do not stop the broadcast.
func (n *Network) BroadcastTransaction(tx chain.Transaction) {
	body, err := json.Marshal(map[string]interface{}{
		"from":      tx.From,
		"to":        tx.To,
		"amount":    tx.Amount,
		"signature": tx.Signature,
	})
	if err != nil {
		log.Printf("broadcast marshal error: %v", err)
		return
	}

	peers := n.GetPeers()
	for _, peer := range peers {
		go func(peerURL string) {
			url := peerURL + "/transaction"
			resp, err := n.client.Post(url, "application/json", bytes.NewReader(body))
			if err != nil {
				log.Printf("broadcast to %s failed: %v", peerURL, err)
				return
			}
			resp.Body.Close()
			log.Printf("📤 Broadcast to %s: status %d", peerURL, resp.StatusCode)
		}(peer)
	}
}

// SyncChain fetches the chain from every peer and adopts the longest valid one.
// Returns true if the local chain was replaced.
func (n *Network) SyncChain(l *ledger.Ledger) bool {
	peers := n.GetPeers()
	replaced := false

	for _, peer := range peers {
		peerChain, err := n.fetchChain(peer)
		if err != nil {
			log.Printf("sync from %s failed: %v", peer, err)
			continue
		}
		if l.ReplaceChain(peerChain) {
			log.Printf("🔄 Chain replaced from peer %s (length %d)", peer, peerChain.Len())
			replaced = true
		}
	}
	return replaced
}

// fetchChain retrieves the blockchain JSON from a single peer.
func (n *Network) fetchChain(peerURL string) (*chain.Blockchain, error) {
	url := peerURL + "/chain"
	resp, err := n.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	bc, err := chain.FromJSON(data)
	if err != nil {
		return nil, fmt.Errorf("decode chain: %w", err)
	}
	return bc, nil
}
