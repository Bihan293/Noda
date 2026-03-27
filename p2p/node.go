package p2p

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Bihan293/Noda/block"
	"github.com/Bihan293/Noda/chain"
	"github.com/Bihan293/Noda/ledger"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	// ProtocolVersion is the current P2P protocol version.
	ProtocolVersion uint32 = 1

	// UserAgent identifies this node implementation.
	UserAgent = "/Noda:0.4.0/"

	// MaxOutboundPeers is the maximum number of outbound connections.
	MaxOutboundPeers = 8

	// MaxInboundPeers is the maximum number of inbound connections.
	MaxInboundPeers = 32

	// MaxGetBlocksLimit is the max number of blocks per getblocks response.
	MaxGetBlocksLimit = 500

	// PingInterval is how often we send pings to keep connections alive.
	PingInterval = 2 * time.Minute

	// PingTimeout is how long we wait for a pong before disconnecting.
	PingTimeout = 30 * time.Second

	// HandshakeTimeout is how long to wait for the handshake to complete.
	HandshakeTimeout = 10 * time.Second

	// ReconnectInterval is the time between reconnection attempts.
	ReconnectInterval = 30 * time.Second

	// BanDuration is how long a misbehaving peer is banned.
	BanDuration = 24 * time.Hour

	// MaxBanScore is the score at which a peer gets banned.
	MaxBanScore = 100
)

// ──────────────────────────────────────────────────────────────────────────────
// Peer
// ──────────────────────────────────────────────────────────────────────────────

// PeerState represents the connection state of a peer.
type PeerState int

const (
	PeerStateConnecting PeerState = iota
	PeerStateHandshaking
	PeerStateActive
	PeerStateDisconnected
)

// Peer represents a connected remote node.
type Peer struct {
	Conn       net.Conn
	Addr       string    // host:port
	NodeID     string    // remote node's unique ID
	Version    uint32    // remote protocol version
	BestHeight uint64    // remote best block height
	State      PeerState // connection state
	Inbound    bool      // true if the peer connected to us
	LastPing   time.Time // last ping sent
	LastPong   time.Time // last pong received
	PingNonce  uint64    // current outstanding ping nonce
	BanScore   int       // misbehavior score
	CreatedAt  time.Time // connection established time

	// Inventory tracking — hashes we know this peer has.
	KnownBlocks map[string]bool
	KnownTxs    map[string]bool

	mu   sync.RWMutex
	done chan struct{} // closed when peer is disconnected
}

// NewPeer creates a new peer from a connection.
func NewPeer(conn net.Conn, inbound bool) *Peer {
	return &Peer{
		Conn:        conn,
		Addr:        conn.RemoteAddr().String(),
		State:       PeerStateConnecting,
		Inbound:     inbound,
		CreatedAt:   time.Now(),
		KnownBlocks: make(map[string]bool),
		KnownTxs:    make(map[string]bool),
		done:        make(chan struct{}),
	}
}

// Send sends a message to the peer.
func (p *Peer) Send(msg *Message) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.State == PeerStateDisconnected {
		return fmt.Errorf("peer %s is disconnected", p.Addr)
	}
	return WriteMessage(p.Conn, msg)
}

// Disconnect closes the connection and marks the peer as disconnected.
func (p *Peer) Disconnect() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.State == PeerStateDisconnected {
		return
	}
	p.State = PeerStateDisconnected
	p.Conn.Close()
	close(p.done)
}

// AddBanScore increases the peer's ban score. Returns true if the peer should be banned.
func (p *Peer) AddBanScore(score int, reason string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BanScore += score
	if p.BanScore >= MaxBanScore {
		log.Printf("[P2P] Peer %s banned (score: %d, reason: %s)", p.Addr, p.BanScore, reason)
		return true
	}
	log.Printf("[P2P] Peer %s ban score: %d (+%d: %s)", p.Addr, p.BanScore, score, reason)
	return false
}

// MarkKnownBlock records that this peer has a block.
func (p *Peer) MarkKnownBlock(hash string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.KnownBlocks[hash] = true
}

// MarkKnownTx records that this peer has a transaction.
func (p *Peer) MarkKnownTx(hash string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.KnownTxs[hash] = true
}

// HasBlock checks if we know this peer has a block.
func (p *Peer) HasBlock(hash string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.KnownBlocks[hash]
}

// HasTx checks if we know this peer has a transaction.
func (p *Peer) HasTx(hash string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.KnownTxs[hash]
}

// ──────────────────────────────────────────────────────────────────────────────
// Node — the P2P network layer
// ──────────────────────────────────────────────────────────────────────────────

// Node manages TCP peer connections and implements the P2P protocol.
type Node struct {
	listenPort uint16
	nodeID     string
	ledger     *ledger.Ledger
	listener   net.Listener

	peers      map[string]*Peer // addr -> peer
	banned     map[string]time.Time // addr -> ban expiry
	seedAddrs  []string // initial seed addresses to connect to
	mu         sync.RWMutex

	quit chan struct{}
	wg   sync.WaitGroup
}

// NewNode creates a new P2P node.
func NewNode(listenPort uint16, l *ledger.Ledger, seeds []string) *Node {
	return &Node{
		listenPort: listenPort,
		nodeID:     generateNodeID(),
		ledger:     l,
		peers:      make(map[string]*Peer),
		banned:     make(map[string]time.Time),
		seedAddrs:  seeds,
		quit:       make(chan struct{}),
	}
}

// generateNodeID creates a random 16-byte hex node identifier.
func generateNodeID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Start begins listening for incoming connections and connects to seed peers.
func (n *Node) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", n.listenPort)
	var err error
	n.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("P2P listen failed on %s: %w", addr, err)
	}

	log.Printf("[P2P] Listening on %s (node ID: %s)", addr, n.nodeID[:16])

	// Accept incoming connections.
	n.wg.Add(1)
	go n.acceptLoop()

	// Connect to seed peers.
	n.wg.Add(1)
	go n.connectToSeeds()

	// Reconnection loop.
	n.wg.Add(1)
	go n.reconnectLoop()

	return nil
}

// Stop shuts down the P2P node.
func (n *Node) Stop() {
	close(n.quit)
	if n.listener != nil {
		n.listener.Close()
	}

	// Disconnect all peers.
	n.mu.RLock()
	for _, peer := range n.peers {
		peer.Disconnect()
	}
	n.mu.RUnlock()

	n.wg.Wait()
	log.Println("[P2P] Node stopped")
}

// ──────────────────────────────────────────────────────────────────────────────
// Connection Management
// ──────────────────────────────────────────────────────────────────────────────

// acceptLoop accepts incoming TCP connections.
func (n *Node) acceptLoop() {
	defer n.wg.Done()
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			select {
			case <-n.quit:
				return
			default:
				log.Printf("[P2P] Accept error: %v", err)
				continue
			}
		}

		remoteAddr := conn.RemoteAddr().String()

		// Check if banned.
		if n.isBanned(remoteAddr) {
			log.Printf("[P2P] Rejected banned peer: %s", remoteAddr)
			conn.Close()
			continue
		}

		// Check inbound limit.
		if n.inboundCount() >= MaxInboundPeers {
			log.Printf("[P2P] Inbound limit reached, rejecting %s", remoteAddr)
			conn.Close()
			continue
		}

		log.Printf("[P2P] Inbound connection from %s", remoteAddr)
		peer := NewPeer(conn, true)
		n.addPeer(peer)

		n.wg.Add(1)
		go n.handlePeer(peer)
	}
}

// connectToSeeds connects to all seed addresses.
func (n *Node) connectToSeeds() {
	defer n.wg.Done()
	for _, addr := range n.seedAddrs {
		select {
		case <-n.quit:
			return
		default:
		}
		n.connectOutbound(addr)
	}
}

// connectOutbound establishes an outbound connection to a peer.
func (n *Node) connectOutbound(addr string) {
	if n.isBanned(addr) {
		return
	}

	// Check if already connected.
	n.mu.RLock()
	if _, exists := n.peers[addr]; exists {
		n.mu.RUnlock()
		return
	}
	n.mu.RUnlock()

	// Check outbound limit.
	if n.outboundCount() >= MaxOutboundPeers {
		return
	}

	log.Printf("[P2P] Connecting to %s...", addr)
	conn, err := net.DialTimeout("tcp", addr, HandshakeTimeout)
	if err != nil {
		log.Printf("[P2P] Failed to connect to %s: %v", addr, err)
		return
	}

	peer := NewPeer(conn, false)
	n.addPeer(peer)

	n.wg.Add(1)
	go n.handlePeer(peer)
}

// reconnectLoop periodically reconnects to seed peers.
func (n *Node) reconnectLoop() {
	defer n.wg.Done()
	ticker := time.NewTicker(ReconnectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.quit:
			return
		case <-ticker.C:
			for _, addr := range n.seedAddrs {
				n.connectOutbound(addr)
			}
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Peer Management
// ──────────────────────────────────────────────────────────────────────────────

func (n *Node) addPeer(p *Peer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.peers[p.Addr] = p
}

func (n *Node) removePeer(p *Peer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.peers, p.Addr)
}

func (n *Node) getPeer(addr string) *Peer {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.peers[addr]
}

func (n *Node) inboundCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	count := 0
	for _, p := range n.peers {
		if p.Inbound && p.State != PeerStateDisconnected {
			count++
		}
	}
	return count
}

func (n *Node) outboundCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	count := 0
	for _, p := range n.peers {
		if !p.Inbound && p.State != PeerStateDisconnected {
			count++
		}
	}
	return count
}

func (n *Node) isBanned(addr string) bool {
	host, _, _ := net.SplitHostPort(addr)
	if host == "" {
		host = addr
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	expiry, ok := n.banned[host]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		// Ban expired — clean up (will be done on next write lock).
		return false
	}
	return true
}

func (n *Node) banPeer(p *Peer) {
	host, _, _ := net.SplitHostPort(p.Addr)
	if host == "" {
		host = p.Addr
	}
	n.mu.Lock()
	n.banned[host] = time.Now().Add(BanDuration)
	n.mu.Unlock()
	p.Disconnect()
	n.removePeer(p)
	log.Printf("[P2P] Banned peer %s for %s", p.Addr, BanDuration)
}

// GetPeers returns addresses of all connected peers (for HTTP API compatibility).
func (n *Node) GetPeers() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	addrs := make([]string, 0, len(n.peers))
	for addr, p := range n.peers {
		if p.State == PeerStateActive {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

// PeerCount returns the number of active peers.
func (n *Node) PeerCount() int {
	return len(n.GetPeers())
}

// ──────────────────────────────────────────────────────────────────────────────
// Peer Handler — main message loop
// ──────────────────────────────────────────────────────────────────────────────

// handlePeer manages the lifecycle of a single peer connection.
func (n *Node) handlePeer(p *Peer) {
	defer n.wg.Done()
	defer func() {
		p.Disconnect()
		n.removePeer(p)
		log.Printf("[P2P] Peer %s disconnected", p.Addr)
	}()

	// Perform handshake.
	if err := n.doHandshake(p); err != nil {
		log.Printf("[P2P] Handshake with %s failed: %v", p.Addr, err)
		return
	}

	log.Printf("[P2P] Peer %s connected (version: %d, height: %d, node: %s)",
		p.Addr, p.Version, p.BestHeight, shortID(p.NodeID))

	// Start ping loop.
	n.wg.Add(1)
	go n.pingLoop(p)

	// Trigger IBD if peer has more blocks.
	localHeight := n.ledger.GetChainHeight()
	if p.BestHeight > localHeight {
		log.Printf("[P2P] Peer %s has more blocks (theirs: %d, ours: %d) — starting IBD",
			p.Addr, p.BestHeight, localHeight)
		n.requestBlocks(p)
	}

	// Main message loop.
	for {
		select {
		case <-n.quit:
			return
		case <-p.done:
			return
		default:
		}

		msg, err := ReadMessage(p.Conn)
		if err != nil {
			// Check if it's a timeout (non-fatal, we keep the connection).
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Printf("[P2P] Read error from %s: %v", p.Addr, err)
			return
		}

		if err := n.handleMessage(p, msg); err != nil {
			log.Printf("[P2P] Error handling %s from %s: %v", msg.Command, p.Addr, err)
			if banned := p.AddBanScore(10, err.Error()); banned {
				n.banPeer(p)
				return
			}
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Handshake
// ──────────────────────────────────────────────────────────────────────────────

// doHandshake performs the version/verack exchange.
func (n *Node) doHandshake(p *Peer) error {
	p.mu.Lock()
	p.State = PeerStateHandshaking
	p.mu.Unlock()

	versionPayload := &VersionPayload{
		Version:    ProtocolVersion,
		BestHeight: n.ledger.GetChainHeight(),
		ListenPort: n.listenPort,
		UserAgent:  UserAgent,
		Timestamp:  time.Now().Unix(),
		NodeID:     n.nodeID,
	}

	// Outbound: we send version first.
	if !p.Inbound {
		if err := n.sendVersion(p, versionPayload); err != nil {
			return fmt.Errorf("send version: %w", err)
		}

		// Wait for their version.
		msg, err := n.readWithTimeout(p.Conn, HandshakeTimeout)
		if err != nil {
			return fmt.Errorf("read version: %w", err)
		}
		if msg.Command != CmdVersion {
			return fmt.Errorf("expected version, got %s", msg.Command)
		}
		if err := n.processVersion(p, msg); err != nil {
			return err
		}

		// Send verack.
		if err := n.sendVerack(p); err != nil {
			return fmt.Errorf("send verack: %w", err)
		}

		// Wait for verack.
		msg, err = n.readWithTimeout(p.Conn, HandshakeTimeout)
		if err != nil {
			return fmt.Errorf("read verack: %w", err)
		}
		if msg.Command != CmdVerack {
			return fmt.Errorf("expected verack, got %s", msg.Command)
		}
	} else {
		// Inbound: wait for their version first.
		msg, err := n.readWithTimeout(p.Conn, HandshakeTimeout)
		if err != nil {
			return fmt.Errorf("read version: %w", err)
		}
		if msg.Command != CmdVersion {
			return fmt.Errorf("expected version, got %s", msg.Command)
		}
		if err := n.processVersion(p, msg); err != nil {
			return err
		}

		// Send our version.
		if err := n.sendVersion(p, versionPayload); err != nil {
			return fmt.Errorf("send version: %w", err)
		}

		// Send verack.
		if err := n.sendVerack(p); err != nil {
			return fmt.Errorf("send verack: %w", err)
		}

		// Wait for verack.
		msg, err = n.readWithTimeout(p.Conn, HandshakeTimeout)
		if err != nil {
			return fmt.Errorf("read verack: %w", err)
		}
		if msg.Command != CmdVerack {
			return fmt.Errorf("expected verack, got %s", msg.Command)
		}
	}

	p.mu.Lock()
	p.State = PeerStateActive
	p.mu.Unlock()

	return nil
}

func (n *Node) sendVersion(p *Peer, vp *VersionPayload) error {
	msg, err := NewMessage(CmdVersion, vp)
	if err != nil {
		return err
	}
	return p.Send(msg)
}

func (n *Node) sendVerack(p *Peer) error {
	msg := &Message{Command: CmdVerack}
	return p.Send(msg)
}

func (n *Node) processVersion(p *Peer, msg *Message) error {
	var vp VersionPayload
	if err := DecodePayload(msg.Payload, &vp); err != nil {
		return fmt.Errorf("decode version payload: %w", err)
	}

	// Reject self-connections.
	if vp.NodeID == n.nodeID {
		return fmt.Errorf("detected self-connection")
	}

	p.mu.Lock()
	p.Version = vp.Version
	p.BestHeight = vp.BestHeight
	p.NodeID = vp.NodeID
	p.mu.Unlock()

	return nil
}

func (n *Node) readWithTimeout(conn net.Conn, timeout time.Duration) (*Message, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	return ReadMessage(conn)
}

// ──────────────────────────────────────────────────────────────────────────────
// Message Handling
// ──────────────────────────────────────────────────────────────────────────────

// handleMessage dispatches incoming messages to the appropriate handler.
func (n *Node) handleMessage(p *Peer, msg *Message) error {
	switch msg.Command {
	case CmdPing:
		return n.handlePing(p, msg)
	case CmdPong:
		return n.handlePong(p, msg)
	case CmdInv:
		return n.handleInv(p, msg)
	case CmdGetData:
		return n.handleGetData(p, msg)
	case CmdTx:
		return n.handleTx(p, msg)
	case CmdBlock:
		return n.handleBlock(p, msg)
	case CmdGetBlocks:
		return n.handleGetBlocks(p, msg)
	case CmdAddr:
		return n.handleAddr(p, msg)
	case CmdVersion:
		// Duplicate version — misbehaving.
		return fmt.Errorf("unexpected duplicate version message")
	case CmdVerack:
		// Duplicate verack — misbehaving.
		return fmt.Errorf("unexpected duplicate verack message")
	default:
		log.Printf("[P2P] Unknown command from %s: %s", p.Addr, msg.Command)
		return nil
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Ping / Pong
// ──────────────────────────────────────────────────────────────────────────────

func (n *Node) pingLoop(p *Peer) {
	defer n.wg.Done()
	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.quit:
			return
		case <-p.done:
			return
		case <-ticker.C:
			nonce := generateNonce()
			p.mu.Lock()
			p.PingNonce = nonce
			p.LastPing = time.Now()
			p.mu.Unlock()

			msg, _ := NewMessage(CmdPing, &PingPayload{Nonce: nonce})
			if err := p.Send(msg); err != nil {
				log.Printf("[P2P] Ping to %s failed: %v", p.Addr, err)
				return
			}
		}
	}
}

func (n *Node) handlePing(p *Peer, msg *Message) error {
	var pp PingPayload
	if err := DecodePayload(msg.Payload, &pp); err != nil {
		return fmt.Errorf("decode ping: %w", err)
	}
	// Respond with pong using the same nonce.
	resp, _ := NewMessage(CmdPong, &PingPayload{Nonce: pp.Nonce})
	return p.Send(resp)
}

func (n *Node) handlePong(p *Peer, msg *Message) error {
	var pp PingPayload
	if err := DecodePayload(msg.Payload, &pp); err != nil {
		return fmt.Errorf("decode pong: %w", err)
	}
	p.mu.Lock()
	if pp.Nonce == p.PingNonce {
		p.LastPong = time.Now()
	}
	p.mu.Unlock()
	return nil
}

func generateNonce() uint64 {
	b := make([]byte, 8)
	rand.Read(b)
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// ──────────────────────────────────────────────────────────────────────────────
// Inventory (inv / getdata)
// ──────────────────────────────────────────────────────────────────────────────

func (n *Node) handleInv(p *Peer, msg *Message) error {
	var inv InvPayload
	if err := DecodePayload(msg.Payload, &inv); err != nil {
		return fmt.Errorf("decode inv: %w", err)
	}

	// Determine which items we need.
	var needed []InvItem
	for _, item := range inv.Items {
		switch item.Type {
		case InvTypeBlock:
			p.MarkKnownBlock(item.Hash)
			// Request blocks we don't have.
			bc := n.ledger.GetChain()
			if bc.GetBlock(0) != nil { // chain exists
				found := false
				for _, b := range bc.Blocks {
					if b.Hash == item.Hash {
						found = true
						break
					}
				}
				if !found {
					needed = append(needed, item)
				}
			}
		case InvTypeTx:
			p.MarkKnownTx(item.Hash)
			// We request TXs we haven't seen.
			if !n.ledger.Mempool.Has(item.Hash) {
				needed = append(needed, item)
			}
		}
	}

	if len(needed) > 0 {
		resp, err := NewMessage(CmdGetData, &InvPayload{Items: needed})
		if err != nil {
			return err
		}
		return p.Send(resp)
	}

	return nil
}

func (n *Node) handleGetData(p *Peer, msg *Message) error {
	var inv InvPayload
	if err := DecodePayload(msg.Payload, &inv); err != nil {
		return fmt.Errorf("decode getdata: %w", err)
	}

	bc := n.ledger.GetChain()

	for _, item := range inv.Items {
		switch item.Type {
		case InvTypeBlock:
			// Find the block by hash.
			for _, b := range bc.Blocks {
				if b.Hash == item.Hash {
					resp, err := NewMessage(CmdBlock, b)
					if err != nil {
						return err
					}
					if err := p.Send(resp); err != nil {
						return fmt.Errorf("send block: %w", err)
					}
					break
				}
			}
		case InvTypeTx:
			// Look in mempool.
			tx := n.ledger.Mempool.Get(item.Hash)
			if tx != nil {
				resp, err := NewMessage(CmdTx, tx)
				if err != nil {
					return err
				}
				if err := p.Send(resp); err != nil {
					return fmt.Errorf("send tx: %w", err)
				}
			}
		}
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Transaction Relay
// ──────────────────────────────────────────────────────────────────────────────

func (n *Node) handleTx(p *Peer, msg *Message) error {
	var tx block.Transaction
	if err := DecodePayload(msg.Payload, &tx); err != nil {
		return fmt.Errorf("decode tx: %w", err)
	}

	p.MarkKnownTx(tx.ID)

	// Submit to our ledger.
	if err := n.ledger.SubmitTransaction(tx); err != nil {
		// Not an error worth banning for — just log and skip.
		log.Printf("[P2P] TX from %s rejected: %v", p.Addr, err)
		return nil
	}

	log.Printf("[P2P] TX accepted from %s: %s -> %s (%.2f)",
		p.Addr, shortAddr(tx.From), shortAddr(tx.To), tx.Amount)

	// Relay to other peers who don't know about it.
	n.broadcastTx(&tx, p)

	return nil
}

// BroadcastTransaction announces a transaction to all connected peers.
func (n *Node) BroadcastTransaction(tx block.Transaction) {
	n.broadcastTx(&tx, nil)
}

func (n *Node) broadcastTx(tx *block.Transaction, exclude *Peer) {
	inv := &InvPayload{
		Items: []InvItem{{Type: InvTypeTx, Hash: tx.ID}},
	}
	msg, err := NewMessage(CmdInv, inv)
	if err != nil {
		log.Printf("[P2P] Failed to create inv message for TX: %v", err)
		return
	}

	n.mu.RLock()
	defer n.mu.RUnlock()

	for _, p := range n.peers {
		if p == exclude || p.State != PeerStateActive {
			continue
		}
		if p.HasTx(tx.ID) {
			continue
		}
		go p.Send(msg)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Block Relay
// ──────────────────────────────────────────────────────────────────────────────

func (n *Node) handleBlock(p *Peer, msg *Message) error {
	var b block.Block
	if err := DecodePayload(msg.Payload, &b); err != nil {
		return fmt.Errorf("decode block: %w", err)
	}

	p.MarkKnownBlock(b.Hash)

	log.Printf("[P2P] Received block from %s: height=%d hash=%s",
		p.Addr, b.Header.Height, shortHash(b.Hash))

	// Try to add the block.
	bc := n.ledger.GetChain()
	expectedHeight := bc.Height() + 1

	if b.Header.Height != expectedHeight {
		// Out of order — might need to request missing blocks.
		if b.Header.Height > expectedHeight {
			log.Printf("[P2P] Block %d from %s is ahead (expected %d) — requesting missing blocks",
				b.Header.Height, p.Addr, expectedHeight)
			n.requestBlocks(p)
		}
		return nil
	}

	// Validate and add block to chain via ledger.
	if err := n.addBlockToLedger(&b); err != nil {
		log.Printf("[P2P] Block %d from %s rejected: %v", b.Header.Height, p.Addr, err)
		if banned := p.AddBanScore(20, "invalid block"); banned {
			n.banPeer(p)
		}
		return nil
	}

	log.Printf("[P2P] Block %d accepted from %s (hash: %s)",
		b.Header.Height, p.Addr, shortHash(b.Hash))

	// Relay to other peers.
	n.broadcastBlock(&b, p)

	return nil
}

// addBlockToLedger adds a validated block to the ledger.
func (n *Node) addBlockToLedger(b *block.Block) error {
	bc := n.ledger.GetChain()

	// Add block to chain (validates header + PoW + merkle).
	if err := bc.AddBlock(b); err != nil {
		return err
	}

	// Apply block to UTXO set.
	if err := n.ledger.UTXOSet.ApplyBlock(b); err != nil {
		return fmt.Errorf("UTXO apply failed: %w", err)
	}

	// Remove confirmed transactions from mempool.
	for _, tx := range b.Transactions {
		n.ledger.Mempool.Remove(tx.ID)
	}

	// Save ledger.
	_ = n.ledger.Save()
	return nil
}

// BroadcastBlock announces a new block to all connected peers.
func (n *Node) BroadcastBlock(b *block.Block) {
	n.broadcastBlock(b, nil)
}

func (n *Node) broadcastBlock(b *block.Block, exclude *Peer) {
	inv := &InvPayload{
		Items: []InvItem{{Type: InvTypeBlock, Hash: b.Hash}},
	}
	msg, err := NewMessage(CmdInv, inv)
	if err != nil {
		log.Printf("[P2P] Failed to create inv message for block: %v", err)
		return
	}

	n.mu.RLock()
	defer n.mu.RUnlock()

	for _, p := range n.peers {
		if p == exclude || p.State != PeerStateActive {
			continue
		}
		if p.HasBlock(b.Hash) {
			continue
		}
		go p.Send(msg)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GetBlocks / Initial Block Download (IBD)
// ──────────────────────────────────────────────────────────────────────────────

// requestBlocks sends a getblocks message to a peer to start IBD.
func (n *Node) requestBlocks(p *Peer) {
	bc := n.ledger.GetChain()
	fromHash := bc.LastHash()

	payload := &GetBlocksPayload{
		FromHash: fromHash,
		Limit:    MaxGetBlocksLimit,
	}

	msg, err := NewMessage(CmdGetBlocks, payload)
	if err != nil {
		log.Printf("[P2P] Failed to create getblocks message: %v", err)
		return
	}

	if err := p.Send(msg); err != nil {
		log.Printf("[P2P] Failed to send getblocks to %s: %v", p.Addr, err)
	}
}

func (n *Node) handleGetBlocks(p *Peer, msg *Message) error {
	var payload GetBlocksPayload
	if err := DecodePayload(msg.Payload, &payload); err != nil {
		return fmt.Errorf("decode getblocks: %w", err)
	}

	bc := n.ledger.GetChain()
	blocks := bc.Blocks

	// Find the starting point.
	startIdx := 0
	if payload.FromHash != "" {
		for i, b := range blocks {
			if b.Hash == payload.FromHash {
				startIdx = i + 1 // Start after the known block.
				break
			}
		}
	}

	limit := payload.Limit
	if limit <= 0 || limit > MaxGetBlocksLimit {
		limit = MaxGetBlocksLimit
	}

	// Collect block hashes to send as inv.
	var items []InvItem
	for i := startIdx; i < len(blocks) && len(items) < limit; i++ {
		items = append(items, InvItem{
			Type: InvTypeBlock,
			Hash: blocks[i].Hash,
		})
	}

	if len(items) > 0 {
		resp, err := NewMessage(CmdInv, &InvPayload{Items: items})
		if err != nil {
			return err
		}
		return p.Send(resp)
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Addr — Peer Discovery
// ──────────────────────────────────────────────────────────────────────────────

func (n *Node) handleAddr(p *Peer, msg *Message) error {
	var addrPayload AddrPayload
	if err := DecodePayload(msg.Payload, &addrPayload); err != nil {
		return fmt.Errorf("decode addr: %w", err)
	}

	for _, addr := range addrPayload.Addresses {
		// Don't connect to ourselves.
		if addr.NodeID == n.nodeID {
			continue
		}

		target := fmt.Sprintf("%s:%d", addr.IP, addr.Port)

		// Try to connect if we don't already know this peer.
		go n.connectOutbound(target)
	}

	return nil
}

// ShareAddresses sends our peer list to a specific peer.
func (n *Node) ShareAddresses(p *Peer) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	var addrs []PeerAddress
	for _, peer := range n.peers {
		if peer == p || peer.State != PeerStateActive {
			continue
		}
		host, port, err := net.SplitHostPort(peer.Addr)
		if err != nil {
			continue
		}
		portNum := parsePort(port)
		addrs = append(addrs, PeerAddress{
			IP:        host,
			Port:      portNum,
			Timestamp: time.Now().Unix(),
			NodeID:    peer.NodeID,
		})
	}

	if len(addrs) == 0 {
		return
	}

	msg, err := NewMessage(CmdAddr, &AddrPayload{Addresses: addrs})
	if err != nil {
		return
	}
	p.Send(msg)
}

// ──────────────────────────────────────────────────────────────────────────────
// SyncChain — for backward compatibility with network.Network interface
// ──────────────────────────────────────────────────────────────────────────────

// SyncChain triggers a chain sync from all connected peers.
// Uses the TCP protocol's getblocks mechanism.
func (n *Node) SyncChain() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	synced := false
	for _, p := range n.peers {
		if p.State != PeerStateActive {
			continue
		}
		if p.BestHeight > n.ledger.GetChainHeight() {
			n.requestBlocks(p)
			synced = true
		}
	}
	return synced
}

// AddPeer adds a new peer address for outbound connection (HTTP API compat).
func (n *Node) AddPeer(addr string) {
	// Add to seeds so reconnect loop picks it up.
	n.mu.Lock()
	n.seedAddrs = append(n.seedAddrs, addr)
	n.mu.Unlock()

	go n.connectOutbound(addr)
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func shortAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-4:]
}

func shortHash(hash string) string {
	if len(hash) <= 16 {
		return hash
	}
	return hash[:16] + "..."
}

func shortID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:16] + "..."
}

func parsePort(s string) uint16 {
	var port uint16
	fmt.Sscanf(s, "%d", &port)
	return port
}

// ensure chain is used (imported)
var _ = (*chain.Blockchain)(nil)
