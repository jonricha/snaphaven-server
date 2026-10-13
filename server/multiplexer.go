package main

import (
	"log"
	"net"
	"sync"

	"github.com/soheilhy/cmux"
)

// UnifiedMultiplexer multiplexes TLS and plain HTTP traffic over a single TCP listener on the same port.
type UnifiedMultiplexer struct {
	rootListener net.Listener
	mux          cmux.CMux
	tlsListener  net.Listener
	httpListener net.Listener
	port         int
	mu           sync.Mutex
	closed       bool
}

// NewUnifiedMultiplexer creates and configures the protocol matchers on root.
func NewUnifiedMultiplexer(root net.Listener, port int) *UnifiedMultiplexer {
	m := cmux.New(root)
	// Match TLS handshakes first
	tlsL := m.Match(cmux.TLS())
	// Match all non-TLS traffic (standard HTTP/1.1)
	httpL := m.Match(cmux.Any())

	return &UnifiedMultiplexer{
		rootListener: root,
		mux:          m,
		tlsListener:  tlsL,
		httpListener: httpL,
		port:         port,
	}
}

// Start begins serving the multiplexer in the background.
func (u *UnifiedMultiplexer) Start() {
	go func() {
		if err := u.mux.Serve(); err != nil && !u.isClosed() {
			log.Printf("Notice: Unified multiplexer stopped: %v", err)
		}
	}()
}

// TLSListener returns the sub-listener that receives TLS traffic (for mTLS gRPC).
func (u *UnifiedMultiplexer) TLSListener() net.Listener {
	return u.tlsListener
}

// HTTPListener returns the sub-listener that receives cleartext HTTP traffic (for Web Setup & Pairing).
func (u *UnifiedMultiplexer) HTTPListener() net.Listener {
	return u.httpListener
}

// Port returns the bound TCP port.
func (u *UnifiedMultiplexer) Port() int {
	return u.port
}

func (u *UnifiedMultiplexer) isClosed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed
}

// Close gracefully closes the multiplexer and underlying listener.
func (u *UnifiedMultiplexer) Close() error {
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return nil
	}
	u.closed = true
	u.mu.Unlock()

	u.mux.Close()
	return u.rootListener.Close()
}
