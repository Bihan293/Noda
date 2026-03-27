// Package api provides the HTTP server with JSON endpoints for interacting
// with the cryptocurrency node. All endpoints return JSON responses.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/Bihan293/Noda/chain"
	"github.com/Bihan293/Noda/crypto"
	"github.com/Bihan293/Noda/ledger"
	"github.com/Bihan293/Noda/network"
)

// Server wraps the ledger and network layer to serve HTTP requests.
type Server struct {
	Ledger  *ledger.Ledger
	Network *network.Network
	Port    string
}

// jsonResponse is a helper to write JSON with a status code.
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// errorResponse sends a JSON error message.
func errorResponse(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

// ---------- Request / Response types ----------

// TransactionRequest is the JSON body for POST /transaction.
type TransactionRequest struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Signature string  `json:"signature"`
}

// KeyPairResponse is returned by GET /generate-keys.
type KeyPairResponse struct {
	Address    string `json:"address"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

// ---------- Handlers ----------

// handleBalance returns the balance for a given address.
// GET /balance?address=<hex_pubkey>
func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	addr := r.URL.Query().Get("address")
	if addr == "" {
		errorResponse(w, http.StatusBadRequest, "address query parameter required")
		return
	}
	balance := s.Ledger.GetBalance(addr)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"address": addr,
		"balance": balance,
	})
}

// handleTransaction processes a new transaction.
// POST /transaction  — body: {from, to, amount, signature}
func (s *Server) handleTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var req TransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	tx := chain.Transaction{
		From:      req.From,
		To:        req.To,
		Amount:    req.Amount,
		Signature: req.Signature,
	}

	if err := s.Ledger.ProcessTransaction(tx); err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	// Broadcast the valid transaction to all peers.
	go s.Network.BroadcastTransaction(tx)

	jsonResponse(w, http.StatusCreated, map[string]string{
		"message": "transaction accepted",
		"id":      s.Ledger.GetChain().LastHash(),
	})
}

// handleChain returns the full blockchain.
// GET /chain
func (s *Server) handleChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	jsonResponse(w, http.StatusOK, s.Ledger.GetChain())
}

// handleGenerateKeys creates a new key pair for the user.
// GET /generate-keys
func (s *Server) handleGenerateKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "key generation failed")
		return
	}
	jsonResponse(w, http.StatusOK, KeyPairResponse{
		Address:    kp.Address,
		PublicKey:  kp.Address, // same as address for Ed25519
		PrivateKey: fmt.Sprintf("%x", kp.PrivateKey),
	})
}

// handlePeers returns the current list of peer URLs.
// GET /peers
func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	jsonResponse(w, http.StatusOK, map[string][]string{
		"peers": s.Network.GetPeers(),
	})
}

// handleAddPeer registers a new peer.
// POST /peers  — body: {"peer": "http://host:port"}
func (s *Server) handleAddPeer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct {
		Peer string `json:"peer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Peer == "" {
		errorResponse(w, http.StatusBadRequest, "peer URL required")
		return
	}
	s.Network.AddPeer(body.Peer)
	jsonResponse(w, http.StatusCreated, map[string]string{
		"message": "peer added",
		"peer":    body.Peer,
	})
}

// handleSync triggers a chain sync from peers (longest chain rule).
// POST /sync
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	replaced := s.Network.SyncChain(s.Ledger)
	jsonResponse(w, http.StatusOK, map[string]bool{
		"chain_replaced": replaced,
	})
}

// handleStatus returns basic node info.
// GET /status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"port":         s.Port,
		"chain_length": s.Ledger.GetChain().Len(),
		"peers":        len(s.Network.GetPeers()),
	})
}

// Start registers routes and starts the HTTP server on the given port.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Core endpoints
	mux.HandleFunc("/balance", s.handleBalance)
	mux.HandleFunc("/transaction", s.handleTransaction)
	mux.HandleFunc("/chain", s.handleChain)

	// Utility endpoints
	mux.HandleFunc("/generate-keys", s.handleGenerateKeys)
	mux.HandleFunc("/status", s.handleStatus)

	// Peer management
	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handlePeers(w, r)
		case http.MethodPost:
			s.handleAddPeer(w, r)
		default:
			errorResponse(w, http.StatusMethodNotAllowed, "GET or POST only")
		}
	})
	mux.HandleFunc("/sync", s.handleSync)

	addr := ":" + s.Port
	log.Printf("🚀 Node listening on http://0.0.0.0%s", addr)
	return http.ListenAndServe(addr, mux)
}
