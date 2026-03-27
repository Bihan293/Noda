package p2p

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

func TestMagicBytes(t *testing.T) {
	// "NODA" = 0x4E4F4441
	if MagicBytes != 0x4E4F4441 {
		t.Errorf("MagicBytes = 0x%X, want 0x4E4F4441", MagicBytes)
	}
}

func TestHeaderSize(t *testing.T) {
	// 4 (magic) + 12 (command) + 4 (payload length) = 20.
	if HeaderSize != 20 {
		t.Errorf("HeaderSize = %d, want 20", HeaderSize)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Command encoding
// ──────────────────────────────────────────────────────────────────────────────

func TestCommandToBytes_RoundTrip(t *testing.T) {
	cmds := []string{CmdVersion, CmdVerack, CmdPing, CmdPong, CmdInv, CmdGetData, CmdTx, CmdBlock, CmdGetBlocks, CmdAddr}

	for _, cmd := range cmds {
		b := commandToBytes(cmd)
		got := bytesToCommand(b)
		if got != cmd {
			t.Errorf("commandToBytes/bytesToCommand round-trip: got %q, want %q", got, cmd)
		}
	}
}

func TestCommandToBytes_Padding(t *testing.T) {
	b := commandToBytes("ping")
	// Should be 12 bytes, "ping" + 8 zero bytes.
	if len(b) != CommandSize {
		t.Errorf("commandToBytes length = %d, want %d", len(b), CommandSize)
	}
	// Check padding bytes are zero.
	for i := 4; i < CommandSize; i++ {
		if b[i] != 0 {
			t.Errorf("commandToBytes padding[%d] = %d, want 0", i, b[i])
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Message encoding / decoding via pipe
// ──────────────────────────────────────────────────────────────────────────────

func TestWriteReadMessage(t *testing.T) {
	// Create a net.Pipe for testing.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	msg := &Message{
		Command: CmdPing,
		Payload: []byte(`{"nonce":42}`),
	}

	// Write in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- WriteMessage(client, msg)
	}()

	// Read on server side.
	got, err := ReadMessage(server)
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}

	if wErr := <-errCh; wErr != nil {
		t.Fatalf("WriteMessage() error: %v", wErr)
	}

	if got.Command != CmdPing {
		t.Errorf("ReadMessage().Command = %q, want %q", got.Command, CmdPing)
	}
	if string(got.Payload) != `{"nonce":42}` {
		t.Errorf("ReadMessage().Payload = %q, want %q", string(got.Payload), `{"nonce":42}`)
	}
}

func TestWriteReadMessage_EmptyPayload(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	msg := &Message{Command: CmdVerack}

	go WriteMessage(client, msg)

	got, err := ReadMessage(server)
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}
	if got.Command != CmdVerack {
		t.Errorf("Command = %q, want %q", got.Command, CmdVerack)
	}
	if len(got.Payload) != 0 {
		t.Errorf("Payload length = %d, want 0", len(got.Payload))
	}
}

func TestReadMessage_BadMagic(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	// Write a header with wrong magic.
	go func() {
		header := make([]byte, HeaderSize)
		binary.LittleEndian.PutUint32(header[0:4], 0xDEADBEEF) // Wrong magic.
		cmd := commandToBytes(CmdPing)
		copy(header[4:16], cmd[:])
		binary.LittleEndian.PutUint32(header[16:20], 0) // No payload.
		client.Write(header)
	}()

	_, err := ReadMessage(server)
	if err == nil {
		t.Error("ReadMessage() should fail with bad magic bytes")
	}
}

func TestWriteMessage_PayloadTooLarge(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	msg := &Message{
		Command: CmdBlock,
		Payload: make([]byte, MaxPayloadSize+1),
	}

	err := WriteMessage(client, msg)
	if err == nil {
		t.Error("WriteMessage() should fail for payload too large")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Payload helpers
// ──────────────────────────────────────────────────────────────────────────────

func TestEncodeDecodePayload(t *testing.T) {
	original := &PingPayload{Nonce: 12345}

	data, err := EncodePayload(original)
	if err != nil {
		t.Fatalf("EncodePayload() error: %v", err)
	}

	var decoded PingPayload
	err = DecodePayload(data, &decoded)
	if err != nil {
		t.Fatalf("DecodePayload() error: %v", err)
	}
	if decoded.Nonce != 12345 {
		t.Errorf("decoded Nonce = %d, want 12345", decoded.Nonce)
	}
}

func TestNewMessage(t *testing.T) {
	payload := &VersionPayload{
		Version:    1,
		BestHeight: 100,
		ListenPort: 9333,
		UserAgent:  "/test/",
		Timestamp:  1000,
		NodeID:     "test-node",
	}

	msg, err := NewMessage(CmdVersion, payload)
	if err != nil {
		t.Fatalf("NewMessage() error: %v", err)
	}
	if msg.Command != CmdVersion {
		t.Errorf("Command = %q, want %q", msg.Command, CmdVersion)
	}
	if len(msg.Payload) == 0 {
		t.Error("NewMessage() Payload is empty")
	}
}

func TestNewMessage_NilPayload(t *testing.T) {
	msg, err := NewMessage(CmdVerack, nil)
	if err != nil {
		t.Fatalf("NewMessage(nil) error: %v", err)
	}
	if msg.Command != CmdVerack {
		t.Errorf("Command = %q, want %q", msg.Command, CmdVerack)
	}
	if msg.Payload != nil {
		t.Error("Payload should be nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Payload types
// ──────────────────────────────────────────────────────────────────────────────

func TestVersionPayload_RoundTrip(t *testing.T) {
	vp := &VersionPayload{
		Version:    ProtocolVersion,
		BestHeight: 500,
		ListenPort: 9333,
		UserAgent:  UserAgent,
		Timestamp:  time.Now().Unix(),
		NodeID:     "abc123",
	}

	data, _ := EncodePayload(vp)
	var decoded VersionPayload
	DecodePayload(data, &decoded)

	if decoded.Version != vp.Version {
		t.Errorf("Version = %d, want %d", decoded.Version, vp.Version)
	}
	if decoded.BestHeight != vp.BestHeight {
		t.Errorf("BestHeight = %d, want %d", decoded.BestHeight, vp.BestHeight)
	}
}

func TestInvPayload_RoundTrip(t *testing.T) {
	inv := &InvPayload{
		Items: []InvItem{
			{Type: InvTypeTx, Hash: "abc123"},
			{Type: InvTypeBlock, Hash: "def456"},
		},
	}

	data, _ := EncodePayload(inv)
	var decoded InvPayload
	DecodePayload(data, &decoded)

	if len(decoded.Items) != 2 {
		t.Fatalf("Items len = %d, want 2", len(decoded.Items))
	}
	if decoded.Items[0].Type != InvTypeTx {
		t.Errorf("Items[0].Type = %d, want %d", decoded.Items[0].Type, InvTypeTx)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Peer
// ──────────────────────────────────────────────────────────────────────────────

func TestNewPeer(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	p := NewPeer(client, false)
	if p == nil {
		t.Fatal("NewPeer() returned nil")
	}
	if p.State != PeerStateConnecting {
		t.Errorf("initial State = %d, want %d", p.State, PeerStateConnecting)
	}
	if p.Inbound {
		t.Error("Inbound should be false")
	}
	if p.KnownBlocks == nil || p.KnownTxs == nil {
		t.Error("KnownBlocks and KnownTxs should be initialized")
	}
}

func TestPeer_MarkKnown(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	p := NewPeer(client, false)

	p.MarkKnownBlock("block1")
	if !p.HasBlock("block1") {
		t.Error("HasBlock(block1) = false after MarkKnownBlock")
	}
	if p.HasBlock("block2") {
		t.Error("HasBlock(block2) should be false")
	}

	p.MarkKnownTx("tx1")
	if !p.HasTx("tx1") {
		t.Error("HasTx(tx1) = false after MarkKnownTx")
	}
}

func TestPeer_AddBanScore(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	p := NewPeer(client, false)

	// Below threshold.
	banned := p.AddBanScore(50, "test")
	if banned {
		t.Error("should not be banned at score 50")
	}

	// At threshold.
	banned = p.AddBanScore(50, "test")
	if !banned {
		t.Error("should be banned at score 100")
	}
}

func TestPeer_Disconnect(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	p := NewPeer(client, false)
	p.Disconnect()

	if p.State != PeerStateDisconnected {
		t.Errorf("State = %d, want %d", p.State, PeerStateDisconnected)
	}

	// Double disconnect should not panic.
	p.Disconnect()
}

func TestPeer_SendDisconnected(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	p := NewPeer(client, false)
	p.Disconnect()

	msg := &Message{Command: CmdPing}
	err := p.Send(msg)
	if err == nil {
		t.Error("Send() should fail on disconnected peer")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Helper functions
// ──────────────────────────────────────────────────────────────────────────────

func TestGenerateNodeID(t *testing.T) {
	id1 := generateNodeID()
	id2 := generateNodeID()

	if len(id1) != 32 { // 16 bytes = 32 hex chars.
		t.Errorf("node ID length = %d, want 32", len(id1))
	}
	if id1 == id2 {
		t.Error("generateNodeID() should produce unique IDs")
	}
}

func TestShortAddr(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyz123456"
	short := shortAddr(long)
	if short == long {
		t.Error("shortAddr() should truncate long addresses")
	}

	tiny := "abc"
	if shortAddr(tiny) != tiny {
		t.Error("shortAddr() should not truncate short addresses")
	}
}

func TestParsePort(t *testing.T) {
	if parsePort("9333") != 9333 {
		t.Errorf("parsePort(9333) = %d", parsePort("9333"))
	}
	if parsePort("0") != 0 {
		t.Errorf("parsePort(0) = %d", parsePort("0"))
	}
}

func TestReadMessage_FromRawBytes(t *testing.T) {
	// Build a valid raw message.
	var buf bytes.Buffer

	// Magic.
	magic := make([]byte, 4)
	binary.LittleEndian.PutUint32(magic, MagicBytes)
	buf.Write(magic)

	// Command.
	cmd := commandToBytes(CmdPong)
	buf.Write(cmd[:])

	// Payload length.
	payloadData := []byte(`{"nonce":99}`)
	pLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(pLen, uint32(len(payloadData)))
	buf.Write(pLen)

	// Payload.
	buf.Write(payloadData)

	// Create a pipe and write the raw bytes.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		client.Write(buf.Bytes())
	}()

	msg, err := ReadMessage(server)
	if err != nil {
		t.Fatalf("ReadMessage() error: %v", err)
	}
	if msg.Command != CmdPong {
		t.Errorf("Command = %q, want %q", msg.Command, CmdPong)
	}
}
