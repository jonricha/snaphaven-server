package main

import (
	"net"
	"sync"
)

// PausableListener wraps a net.Listener and drops/closes incoming connections when paused.
type PausableListener struct {
	net.Listener
	mu     sync.RWMutex
	paused bool
}

func NewPausableListener(inner net.Listener) *PausableListener {
	return &PausableListener{
		Listener: inner,
		paused:   false,
	}
}

func (p *PausableListener) SetPaused(paused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = paused
}

func (p *PausableListener) IsPaused() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.paused
}

func (p *PausableListener) Accept() (net.Conn, error) {
	for {
		conn, err := p.Listener.Accept()
		if err != nil {
			return nil, err
		}

		p.mu.RLock()
		paused := p.paused
		p.mu.RUnlock()

		if paused {
			// Discard connection immediately when gRPC sync is paused
			_ = conn.Close()
			continue
		}

		return conn, nil
	}
}
