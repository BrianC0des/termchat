package network

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestPeer(conn net.Conn, pending bool) *PeerConnection {
	p := &PeerConnection{Conn: conn, Writer: bufio.NewWriter(conn), Connected: time.Now()}
	if pending {
		p.pending = 1
		atomic.AddInt64(&pendingConns, 1)
	}
	return p
}

func runReadLoop(t *testing.T, p *PeerConnection) (done chan struct{}, cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{ctx: ctx, peers: map[string]*PeerConnection{}}
	done = make(chan struct{})
	go func() { m.readLoop(p); close(done) }()
	return done, cancel
}

func waitClosed(t *testing.T, done chan struct{}, within time.Duration, why string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("connection not closed: %s", why)
	}
}

func TestPreHandshakePacketLimit(t *testing.T) {
	before := atomic.LoadInt64(&pendingConns)
	client, server := net.Pipe()
	defer client.Close()
	done, cancel := runReadLoop(t, newTestPeer(server, true))
	defer cancel()

	// A newline-free stream bigger than the pre-handshake budget (but far
	// below the 10 MB post-handshake limit) must get the connection dropped.
	go func() { _, _ = client.Write([]byte(strings.Repeat("A", maxPreHandshakePacket+4096))) }()
	waitClosed(t, done, 3*time.Second, "oversized pre-handshake packet")
	if got := atomic.LoadInt64(&pendingConns); got != before {
		t.Errorf("pending slot leaked: %d -> %d", before, got)
	}
}

func TestHandshakeTimeout(t *testing.T) {
	old := handshakeTimeout
	handshakeTimeout = 150 * time.Millisecond
	defer func() { handshakeTimeout = old }()

	before := atomic.LoadInt64(&pendingConns)
	client, server := net.Pipe()
	defer client.Close()
	done, cancel := runReadLoop(t, newTestPeer(server, true))
	defer cancel()

	// Peer connects and says nothing: slowloris-style idle connection.
	waitClosed(t, done, 3*time.Second, "silent pre-handshake connection")
	if got := atomic.LoadInt64(&pendingConns); got != before {
		t.Errorf("pending slot leaked: %d -> %d", before, got)
	}
}

func TestReleasePendingOnce(t *testing.T) {
	before := atomic.LoadInt64(&pendingConns)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	p := newTestPeer(c1, true)
	releasePending(p)
	releasePending(p) // second call must be a no-op
	if got := atomic.LoadInt64(&pendingConns); got != before {
		t.Errorf("pendingConns = %d, want %d", got, before)
	}
}

func TestReadBoundedLineLimit(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(strings.Repeat("x", 200) + "\n"))
	if _, err := readBoundedLine(r, 100); err == nil {
		t.Error("expected error for line over limit")
	}
	r = bufio.NewReader(strings.NewReader("ok\n"))
	if line, err := readBoundedLine(r, 100); err != nil || string(line) != "ok\n" {
		t.Errorf("got %q, %v", line, err)
	}
}

func handshakeFrom(id, remote string) (*PeerConnection, *Packet) {
	c1, c2 := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, c2) }()
	p := &PeerConnection{Conn: c1, Writer: bufio.NewWriter(c1), RemoteIP: remote}
	return p, &Packet{Type: MsgTypeHandshake, SenderID: id, Sender: "n-" + id}
}

func newLANManager() *Manager {
	return &Manager{LocalID: "local", peers: map[string]*PeerConnection{}, cloudPeers: map[string]*PeerConnection{}}
}

func TestHandshakeCannotTakeOverExistingIDFromOtherHost(t *testing.T) {
	m := newLANManager()
	real, pkt := handshakeFrom("victim", "10.0.0.5:5000")
	m.handlePacket(real, pkt)

	evil, pkt2 := handshakeFrom("victim", "10.0.0.66:6000")
	m.handlePacket(evil, pkt2)

	if m.peers["victim"] != real {
		t.Fatal("peer entry was taken over from a different host")
	}
	if evil.ID != "" {
		t.Error("rejected connection must not be marked identified")
	}
	// The real peer's connection must still be usable.
	_ = real.Conn.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := real.Conn.Write([]byte("x")); err != nil {
		t.Errorf("legitimate peer was disconnected: %v", err)
	}
}

func TestHandshakeAllowsReconnectFromSameHost(t *testing.T) {
	m := newLANManager()
	first, pkt := handshakeFrom("bob", "10.0.0.5:5000")
	m.handlePacket(first, pkt)
	second, pkt2 := handshakeFrom("bob", "10.0.0.5:5001")
	m.handlePacket(second, pkt2)
	if m.peers["bob"] != second {
		t.Fatal("same-host reconnect should replace the old connection")
	}
}

func TestPerHostAndTotalPeerCaps(t *testing.T) {
	m := newLANManager()
	for i := 0; i < maxPeersPerHost+3; i++ {
		p, pkt := handshakeFrom("id"+strconv.Itoa(i), "10.0.0.9:"+strconv.Itoa(7000+i))
		m.handlePacket(p, pkt)
	}
	if len(m.peers) != maxPeersPerHost {
		t.Fatalf("per-host cap: %d peers registered, want %d", len(m.peers), maxPeersPerHost)
	}

	m = newLANManager()
	for i := 0; i < maxPeers+10; i++ {
		host := "10." + strconv.Itoa(i/250) + "." + strconv.Itoa(i%250) + ".1:9000"
		p, pkt := handshakeFrom("n"+strconv.Itoa(i), host)
		m.handlePacket(p, pkt)
	}
	if len(m.peers) != maxPeers {
		t.Fatalf("total cap: %d peers registered, want %d", len(m.peers), maxPeers)
	}
}
