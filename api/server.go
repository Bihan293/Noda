// Package api provides the HTTP server with JSON endpoints for interacting
// with the cryptocurrency node.
//
// Updated for:
//   - UTXO-based balance queries
//   - Mempool status and pending transactions
//   - Faucet with global 11M cap (no per-address cooldown)
//   - Block-based chain with PoW, halving
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Bihan293/Noda/block"
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

// ---------- Helpers ----------

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

// loggingMiddleware wraps a handler and logs every request with timing.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[HTTP] %s %s — %s", r.Method, r.URL.Path, time.Since(start).Round(time.Microsecond))
	})
}

// ---------- Request / Response types ----------

// TransactionRequest is the JSON body for POST /transaction.
type TransactionRequest struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Signature string  `json:"signature"`
}

// SendRequest is the JSON body for POST /sign and POST /send.
type SendRequest struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Amount     float64 `json:"amount"`
	PrivateKey string  `json:"private_key"`
}

// FaucetRequest is the JSON body for POST /faucet.
type FaucetRequest struct {
	To string `json:"to"`
}

// KeyPairResponse is returned by GET /generate-keys.
type KeyPairResponse struct {
	Address    string `json:"address"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

// ---------- Handlers ----------

// handleBalance returns the balance for a given address (from UTXO set).
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
	pendingSpend := s.Ledger.Mempool.GetSpendingTotal(addr)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"address":         addr,
		"balance":         balance,
		"pending_spend":   pendingSpend,
		"available":       balance - pendingSpend,
		"utxo_count":      len(s.Ledger.UTXOSet.GetUTXOsForAddress(addr)),
	})
}

// handleTransaction processes a pre-signed transaction.
// POST /transaction — body: {from, to, amount, signature}
func (s *Server) handleTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var req TransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[TX] Failed to decode request body: %v", err)
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	log.Printf("[TX] Received: %s -> %s (%.2f coins)", shortAddr(req.From), shortAddr(req.To), req.Amount)

	tx := block.Transaction{
		From:      req.From,
		To:        req.To,
		Amount:    req.Amount,
		Signature: req.Signature,
	}

	if err := s.Ledger.SubmitTransaction(tx); err != nil {
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
	log.Printf("[KEYS] Generated new key pair — address: %s", shortAddr(kp.Address))
	jsonResponse(w, http.StatusOK, KeyPairResponse{
		Address:    kp.Address,
		PublicKey:  kp.Address,
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
// POST /peers — body: {"peer": "http://host:port"}
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

// handleStatus returns node status including block height, mining info, mempool, UTXO, and faucet state.
// GET /status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	ch := s.Ledger.GetChain()
	resp := map[string]interface{}{
		"port":          s.Port,
		"block_height":  ch.Height(),
		"chain_length":  ch.Len(),
		"peers":         len(s.Network.GetPeers()),
		"http_peers":    len(s.Network.GetPeers()),
		"total_mined":   ch.TotalMined,
		"block_reward":  s.Ledger.GetBlockReward(),
		"total_faucet":  ch.TotalFaucet,
		"faucet_active": s.Ledger.IsFaucetActive(),
		"mempool_size":  s.Ledger.GetMempoolSize(),
		"utxo_count":    s.Ledger.UTXOSet.Size(),
		"p2p_peers":     s.Network.PeerCount(),
	}
	if addr := s.Ledger.FaucetAddress(); addr != "" {
		resp["faucet_address"] = addr
		resp["faucet_balance"] = s.Ledger.GetBalance(addr)
		resp["faucet_remaining"] = s.Ledger.FaucetRemaining()
	}
	jsonResponse(w, http.StatusOK, resp)
}

// handleMempool returns the current mempool state.
// GET /mempool
func (s *Server) handleMempool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	pending := s.Ledger.GetPendingTransactions(100)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"size":         s.Ledger.GetMempoolSize(),
		"transactions": pending,
	})
}

// handleSign signs a transaction and returns it without broadcasting.
// POST /sign — body: {from, to, amount, private_key}
func (s *Server) handleSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.PrivateKey == "" {
		errorResponse(w, http.StatusBadRequest, "private_key is required")
		return
	}
	if req.To == "" {
		errorResponse(w, http.StatusBadRequest, "'to' address is required")
		return
	}
	if req.Amount <= 0 {
		errorResponse(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	from := req.From
	if from == "" {
		derived, err := crypto.AddressFromPrivateKey(req.PrivateKey)
		if err != nil {
			errorResponse(w, http.StatusBadRequest, "cannot derive address: "+err.Error())
			return
		}
		from = derived
	}

	sig, err := crypto.SignTransaction(req.PrivateKey, from, req.To, req.Amount)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "signing failed: "+err.Error())
		return
	}

	tx := block.Transaction{
		From:      from,
		To:        req.To,
		Amount:    req.Amount,
		Signature: sig,
	}

	log.Printf("[SIGN] Signed TX: %s -> %s (%.2f coins)", shortAddr(from), shortAddr(req.To), req.Amount)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"transaction": tx,
		"signature":   sig,
	})
}

// handleSend is the all-in-one endpoint: sign + validate + chain + broadcast.
// POST /send — body: {from, to, amount, private_key}
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.PrivateKey == "" {
		errorResponse(w, http.StatusBadRequest, "private_key is required")
		return
	}
	if req.To == "" {
		errorResponse(w, http.StatusBadRequest, "'to' address is required")
		return
	}
	if req.Amount <= 0 {
		errorResponse(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	from := req.From
	if from == "" {
		derived, err := crypto.AddressFromPrivateKey(req.PrivateKey)
		if err != nil {
			errorResponse(w, http.StatusBadRequest, "cannot derive address: "+err.Error())
			return
		}
		from = derived
	}

	sig, err := crypto.SignTransaction(req.PrivateKey, from, req.To, req.Amount)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "signing failed: "+err.Error())
		return
	}

	tx := block.Transaction{
		From:      from,
		To:        req.To,
		Amount:    req.Amount,
		Signature: sig,
	}

	log.Printf("[SEND] Processing: %s -> %s (%.2f coins)", shortAddr(from), shortAddr(req.To), req.Amount)

	if err := s.Ledger.SubmitTransaction(tx); err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	// Broadcast to peers.
	go s.Network.BroadcastTransaction(tx)

	jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"message":     "transaction sent",
		"id":          s.Ledger.GetChain().LastHash(),
		"from":        from,
		"to":          req.To,
		"amount":      req.Amount,
		"signature":   sig,
		"new_balance": s.Ledger.GetBalance(from),
	})
}

// handleFaucet sends free coins to a given address.
// POST /faucet — body: {"to": "..."}
// No per-address cooldown — only global 11M cap applies.
func (s *Server) handleFaucet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var req FaucetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.To == "" {
		errorResponse(w, http.StatusBadRequest, "'to' address is required")
		return
	}

	log.Printf("[FAUCET] Request from %s", shortAddr(req.To))

	tx, err := s.Ledger.ProcessFaucet(req.To)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	// Broadcast faucet transaction to peers.
	go s.Network.BroadcastTransaction(*tx)

	jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"message":          fmt.Sprintf("%.0f coins sent from faucet", tx.Amount),
		"to":               req.To,
		"amount":           tx.Amount,
		"new_balance":      s.Ledger.GetBalance(req.To),
		"faucet_remaining": s.Ledger.FaucetRemaining(),
	})
}

// ---------- Router ----------

// Start registers routes and starts the HTTP server on the given port.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Core endpoints
	mux.HandleFunc("/balance", s.handleBalance)
	mux.HandleFunc("/transaction", s.handleTransaction)
	mux.HandleFunc("/chain", s.handleChain)

	// Convenience endpoints
	mux.HandleFunc("/sign", s.handleSign)
	mux.HandleFunc("/send", s.handleSend)
	mux.HandleFunc("/faucet", s.handleFaucet)

	// Utility endpoints
	mux.HandleFunc("/generate-keys", s.handleGenerateKeys)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/mempool", s.handleMempool)

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
	log.Printf("=== Noda Node listening on http://0.0.0.0%s ===", addr)
	log.Printf("Endpoints: /balance /transaction /chain /sign /send /faucet /generate-keys /status /mempool /peers /sync")

	return http.ListenAndServe(addr, loggingMiddleware(mux))
}

// shortAddr returns the first 8 and last 4 chars of an address for logging.
func shortAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-4:]
}
