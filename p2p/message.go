// Package p2p implements a Bitcoin-style TCP peer-to-peer protocol.
//
// Message framing: [4-byte magic][12-byte command][4-byte payload length][payload]
// All multi-byte integers are little-endian.
//
// Supported commands:
//   - version / verack  — handshake
//   - ping / pong       — keep-alive
//   - inv / getdata     — inventory exchange
//   - tx                — transaction relay
//   - block             — block relay
//   - getblocks         — request block hashes
//   - addr              — peer address exchange
package p2p

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	// MagicBytes identifies messages belonging to the Noda network.
	MagicBytes uint32 = 0x4E4F4441 // "NODA" in ASCII

	// CommandSize is the fixed size of the command field in the header.
	CommandSize = 12

	// HeaderSize = 4 (magic) + 12 (command) + 4 (payload length).
	HeaderSize = 20

	// MaxPayloadSize is the maximum allowed payload size (16 MB).
	MaxPayloadSize = 16 * 1024 * 1024

	// ReadTimeout is how long to wait for a complete message read.
	ReadTimeout = 30 * time.Second

	// WriteTimeout is how long to wait for a complete message write.
	WriteTimeout = 15 * time.Second
)

// ──────────────────────────────────────────────────────────────────────────────
// Command types
// ──────────────────────────────────────────────────────────────────────────────

const (
	CmdVersion    = "version"
	CmdVerack     = "verack"
	CmdPing       = "ping"
	CmdPong       = "pong"
	CmdInv        = "inv"
	CmdGetData    = "getdata"
	CmdTx         = "tx"
	CmdBlock      = "block"
	CmdGetBlocks  = "getblocks"
	CmdAddr       = "addr"
	CmdGetHeaders = "getheaders"  // CRITICAL-4: headers-first sync
	CmdHeaders    = "headers"     // CRITICAL-4: headers-first sync
)

// ──────────────────────────────────────────────────────────────────────────────
// Inventory types
// ──────────────────────────────────────────────────────────────────────────────

const (
	InvTypeTx    uint32 = 1
	InvTypeBlock uint32 = 2
)

// ──────────────────────────────────────────────────────────────────────────────
// Message
// ──────────────────────────────────────────────────────────────────────────────

// Message is a protocol message exchanged between peers.
type Message struct {
	Command string // one of the Cmd* constants
	Payload []byte // JSON-encoded payload (may be nil for verack/pong)
}

// ──────────────────────────────────────────────────────────────────────────────
// Payload types
// ──────────────────────────────────────────────────────────────────────────────

// VersionPayload is sent during the handshake.
type VersionPayload struct {
	Version     uint32 `json:"version"`      // protocol version
	BestHeight  uint64 `json:"best_height"`  // sender's best block height
	ListenPort  uint16 `json:"listen_port"`  // port we listen on for incoming connections
	UserAgent   string `json:"user_agent"`   // e.g. "/Noda:0.4.0/"
	Timestamp   int64  `json:"timestamp"`    // unix timestamp
	NodeID      string `json:"node_id"`      // unique node identifier
}

// PingPayload carries a nonce for ping/pong.
type PingPayload struct {
	Nonce uint64 `json:"nonce"`
}

// InvItem describes a single inventory item (block or transaction).
type InvItem struct {
	Type uint32 `json:"type"` // InvTypeTx or InvTypeBlock
	Hash string `json:"hash"` // hex-encoded hash
}

// InvPayload is the payload for inv and getdata messages.
type InvPayload struct {
	Items []InvItem `json:"items"`
}

// GetBlocksPayload requests block hashes starting from a known hash.
type GetBlocksPayload struct {
	FromHash string `json:"from_hash"` // hash of the last known block (empty = genesis)
	Limit    int    `json:"limit"`     // max number of block hashes to return
}

// AddrPayload carries a list of peer addresses.
type AddrPayload struct {
	Addresses []PeerAddress `json:"addresses"`
}

// PeerAddress describes a reachable peer.
type PeerAddress struct {
	IP        string `json:"ip"`
	Port      uint16 `json:"port"`
	Timestamp int64  `json:"timestamp"` // last known active time
	NodeID    string `json:"node_id"`
}

// GetHeadersPayload requests block headers starting from a known hash.
// CRITICAL-4: headers-first sync support.
type GetHeadersPayload struct {
	FromHash string `json:"from_hash"` // hash of the last known block
	Limit    int    `json:"limit"`     // max number of headers to return
}

// BlockHeaderInfo contains block header metadata for headers-first sync.
type BlockHeaderInfo struct {
	Hash       string `json:"hash"`
	Height     uint64 `json:"height"`
	PrevHash   string `json:"prev_hash"`
	MerkleRoot string `json:"merkle_root"`
	Timestamp  int64  `json:"timestamp"`
	Bits       string `json:"bits"`
	Nonce      uint64 `json:"nonce"`
	TxCount    int    `json:"tx_count"`
}

// HeadersPayload carries block headers for headers-first sync.
type HeadersPayload struct {
	Headers []BlockHeaderInfo `json:"headers"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Wire encoding / decoding
// ──────────────────────────────────────────────────────────────────────────────

// Errors
var (
	ErrBadMagic     = errors.New("p2p: invalid magic bytes")
	ErrPayloadSize  = errors.New("p2p: payload exceeds maximum size")
	ErrReadTimeout  = errors.New("p2p: read timeout")
	ErrWriteTimeout = errors.New("p2p: write timeout")
)

// commandToBytes converts a command string to a fixed-size byte array (padded with zeros).
func commandToBytes(cmd string) [CommandSize]byte {
	var b [CommandSize]byte
	copy(b[:], cmd)
	return b
}

// bytesToCommand converts a fixed-size byte array back to a command string (trimmed).
func bytesToCommand(b [CommandSize]byte) string {
	// Find the first null byte.
	n := 0
	for n < CommandSize && b[n] != 0 {
		n++
	}
	return string(b[:n])
}

// WriteMessage sends a message over a TCP connection.
func WriteMessage(conn net.Conn, msg *Message) error {
	if err := conn.SetWriteDeadline(time.Now().Add(WriteTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}

	payload := msg.Payload
	if len(payload) > MaxPayloadSize {
		return ErrPayloadSize
	}

	// Build header.
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], MagicBytes)
	cmd := commandToBytes(msg.Command)
	copy(header[4:16], cmd[:])
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(payload)))

	// Write header + payload.
	if _, err := conn.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
	}

	return nil
}

// ReadMessage reads a message from a TCP connection.
func ReadMessage(conn net.Conn) (*Message, error) {
	if err := conn.SetReadDeadline(time.Now().Add(ReadTimeout)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	// Read header.
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Verify magic.
	magic := binary.LittleEndian.Uint32(header[0:4])
	if magic != MagicBytes {
		return nil, ErrBadMagic
	}

	// Parse command.
	var cmdBytes [CommandSize]byte
	copy(cmdBytes[:], header[4:16])
	command := bytesToCommand(cmdBytes)

	// Parse payload length.
	payloadLen := binary.LittleEndian.Uint32(header[16:20])
	if payloadLen > MaxPayloadSize {
		return nil, ErrPayloadSize
	}

	// Read payload.
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return nil, fmt.Errorf("read payload: %w", err)
		}
	}

	return &Message{
		Command: command,
		Payload: payload,
	}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Payload helpers
// ──────────────────────────────────────────────────────────────────────────────

// EncodePayload encodes a payload struct to JSON bytes.
func EncodePayload(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// DecodePayload decodes JSON bytes into a payload struct.
func DecodePayload(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// NewMessage creates a new message with an encoded payload.
func NewMessage(command string, payload interface{}) (*Message, error) {
	msg := &Message{Command: command}
	if payload != nil {
		data, err := EncodePayload(payload)
		if err != nil {
			return nil, fmt.Errorf("encode %s payload: %w", command, err)
		}
		msg.Payload = data
	}
	return msg, nil
}
