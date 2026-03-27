// Package api provides the HTTP server with JSON endpoints for interacting
// with the cryptocurrency node.
//
// Production features:
//   - UTXO-based balance queries
//   - Mempool status and pending transactions
//   - Faucet with global 11M cap (no per-address cooldown)
//   - Block-based chain with PoW, halving
//   - Structured logging via log/slog
//   - Prometheus-compatible /metrics endpoint
//   - Rate limiting per IP
//   - Graceful shutdown with context
//   - Input validation and security headers
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Bihan293/Noda/block"
	"github.com/Bihan293/Noda/crypto"
	"github.com/Bihan293/Noda/ledger"
	m "github.com/Bihan293/Noda/metrics"
	"github.com/Bihan293/Noda/network"
	"github.com/Bihan293/Noda/ratelimit"
)

// Server wraps the ledger and network layer to serve HTTP requests.
type Server struct {
	Ledger      *ledger.Ledger
	Network     *network.Network
	Port        string
	RateLimiter *ratelimit.Limiter
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

// securityHeaders adds common security headers to responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware wraps a handler and logs every request with timing.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		m.HTTPRequestsTotal.Inc()
		m.HTTPRequestDuration.Set(float64(duration.Microseconds()) / 1000.0)
		slog.Debug("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", duration.Round(time.Microsecond).String(),
			"remote", r.RemoteAddr,
		)
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

// ---------- Input validation ----------

const (
	maxAddressLen    = 256
	maxSignatureLen  = 256
	maxPrivateKeyLen = 256
	maxBodySize      = 1 << 16 // 64 KB
)

// validateHex checks if a string contains only valid hex characters.
func validateHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// validateAddress checks address format and length.
func validateAddress(addr string) error {
	if addr == "" {
		return fmt.Errorf("address is required")
	}
	if len(addr) > maxAddressLen {
		return fmt.Errorf("address too long (max %d chars)", maxAddressLen)
	}
	if !validateHex(addr) {
		return fmt.Errorf("address must be hex-encoded")
	}
	return nil
}

// ---------- Handlers ----------

// handleHealth is a lightweight health check endpoint.
// GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{
		"status": "ok",
		"node":   "noda",
		"version": "0.5.0",
	})
}

// handleBalance returns the balance for a given address (from UTXO set).
// GET /balance?address=<hex_pubkey>
func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	addr := r.URL.Query().Get("address")
	if err := validateAddress(addr); err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	balance := s.Ledger.GetBalance(addr)
	pendingSpend := s.Ledger.Mempool.GetSpendingTotal(addr)
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"address":       addr,
		"balance":       balance,
		"pending_spend": pendingSpend,
		"available":     balance - pendingSpend,
		"utxo_count":    len(s.Ledger.UTXOSet.GetUTXOsForAddress(addr)),
	})
}

// handleTransaction processes a pre-signed transaction.
// POST /transaction — body: {from, to, amount, signature}
func (s *Server) handleTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req TransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("TX decode failed", "error", err)
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Input validation.
	if err := validateAddress(req.From); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid 'from': "+err.Error())
		return
	}
	if err := validateAddress(req.To); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid 'to': "+err.Error())
		return
	}
	if len(req.Signature) > maxSignatureLen || !validateHex(req.Signature) {
		errorResponse(w, http.StatusBadRequest, "invalid signature format")
		return
	}

	slog.Info("TX received", "from", shortAddr(req.From), "to", shortAddr(req.To), "amount", req.Amount)

	tx := block.Transaction{
		From:      req.From,
		To:        req.To,
		Amount:    req.Amount,
		Signature: req.Signature,
	}

	if err := s.Ledger.SubmitTransaction(tx); err != nil {
		m.TxRejected.Inc()
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	m.TxAccepted.Inc()
	s.updateMetrics()

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
	slog.Debug("Keys generated", "address", shortAddr(kp.Address))
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
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var body struct {
		Peer string `json:"peer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Peer == "" {
		errorResponse(w, http.StatusBadRequest, "peer URL required")
		return
	}
	// Basic URL validation.
	if !strings.HasPrefix(body.Peer, "http://") && !strings.HasPrefix(body.Peer, "https://") {
		errorResponse(w, http.StatusBadRequest, "peer URL must start with http:// or https://")
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
	if replaced {
		s.updateMetrics()
	}
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
		"version":       "0.5.0",
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
		"max_supply":    block.MaxTotalSupply,
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

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.PrivateKey == "" {
		errorResponse(w, http.StatusBadRequest, "private_key is required")
		return
	}
	if len(req.PrivateKey) > maxPrivateKeyLen || !validateHex(req.PrivateKey) {
		errorResponse(w, http.StatusBadRequest, "invalid private_key format")
		return
	}
	if req.To == "" {
		errorResponse(w, http.StatusBadRequest, "'to' address is required")
		return
	}
	if err := validateAddress(req.To); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid 'to': "+err.Error())
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

	slog.Debug("TX signed", "from", shortAddr(from), "to", shortAddr(req.To), "amount", req.Amount)

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

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.PrivateKey == "" {
		errorResponse(w, http.StatusBadRequest, "private_key is required")
		return
	}
	if len(req.PrivateKey) > maxPrivateKeyLen || !validateHex(req.PrivateKey) {
		errorResponse(w, http.StatusBadRequest, "invalid private_key format")
		return
	}
	if req.To == "" {
		errorResponse(w, http.StatusBadRequest, "'to' address is required")
		return
	}
	if err := validateAddress(req.To); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid 'to': "+err.Error())
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

	slog.Info("Processing send", "from", shortAddr(from), "to", shortAddr(req.To), "amount", req.Amount)

	if err := s.Ledger.SubmitTransaction(tx); err != nil {
		m.TxRejected.Inc()
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	m.TxAccepted.Inc()
	s.updateMetrics()

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

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req FaucetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := validateAddress(req.To); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid 'to': "+err.Error())
		return
	}

	slog.Info("Faucet request", "to", shortAddr(req.To))

	tx, err := s.Ledger.ProcessFaucet(req.To)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	s.updateMetrics()

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

// ---------- Metrics update ----------

// updateMetrics syncs all gauge metrics with the current ledger state.
func (s *Server) updateMetrics() {
	ch := s.Ledger.GetChain()
	m.BlockHeight.Set(int64(ch.Height()))
	m.BlockCount.Set(int64(ch.Len()))
	m.TotalMined.Set(ch.TotalMined)
	m.TotalFaucet.Set(ch.TotalFaucet)
	m.BlockReward.Set(s.Ledger.GetBlockReward())
	m.MempoolSize.Set(int64(s.Ledger.GetMempoolSize()))
	m.UTXOCount.Set(int64(s.Ledger.UTXOSet.Size()))
	m.PeerCount.Set(int64(s.Network.PeerCount()))
	m.FaucetRemaining.Set(s.Ledger.FaucetRemaining())
	if s.Ledger.IsFaucetActive() {
		m.FaucetActive.Set(1)
	} else {
		m.FaucetActive.Set(0)
	}
}

// ---------- Router ----------

// Start registers routes and starts the HTTP server on the given port.
// Supports graceful shutdown via the provided context.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Health check (no rate limiting).
	mux.HandleFunc("/health", s.handleHealth)

	// Prometheus metrics endpoint (no rate limiting).
	mux.Handle("/metrics", m.Handler())

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

	// Apply middleware chain: security → rate limiting → logging → routes.
	var handler http.Handler = mux
	if s.RateLimiter != nil {
		handler = s.RateLimiter.Middleware(handler)
	}
	handler = loggingMiddleware(handler)
	handler = securityHeaders(handler)

	addr := ":" + s.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	slog.Info("Noda Node listening",
		"address", "http://0.0.0.0"+addr,
		"endpoints", "/health /metrics /balance /transaction /chain /sign /send /faucet /generate-keys /status /mempool /peers /sync",
	)

	// Graceful shutdown.
	go func() {
		<-ctx.Done()
		slog.Info("Shutting down HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", "error", err)
		}
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// shortAddr returns the first 8 and last 4 chars of an address for logging.
func shortAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-4:]
}
