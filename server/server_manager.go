package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"

	pb "github.com/jonricha/snaphaven-server/snaphaven"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type ServerManager struct {
	mu              sync.Mutex
	configMgr       *ConfigManager
	certMgr         *CertManager
	deviceMgr       *DeviceManager
	grpcServer      *grpc.Server
	pausableLis     *PausableListener
	multiplexer     *UnifiedMultiplexer
	mdnsBroadcaster *MDNSBroadcaster
	setupServer     *SetupServer
	boundPort       int
	isRunning       bool
}

func NewServerManager(cm *ConfigManager, certMgr *CertManager, devMgr *DeviceManager) *ServerManager {
	return &ServerManager{
		configMgr: cm,
		certMgr:   certMgr,
		deviceMgr: devMgr,
	}
}

func (sm *ServerManager) IsRunning() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.isRunning
}

func (sm *ServerManager) BoundPort() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.boundPort
}

func (sm *ServerManager) HTTPListener() net.Listener {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.multiplexer != nil {
		return sm.multiplexer.HTTPListener()
	}
	return nil
}

func (sm *ServerManager) Start() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.isRunning {
		return nil
	}

	// 1. If multiplexer is already initialized and was just paused, unpause gRPC traffic
	if sm.pausableLis != nil && sm.multiplexer != nil {
		sm.pausableLis.SetPaused(false)
		sm.isRunning = true
		log.Printf("🟢 Resumed mTLS gRPC sync service on port :%d", sm.boundPort)
		return nil
	}

	cfg := sm.configMgr.Config

	// 2. Resilient Port Binding with Sequential Fallback Hunting
	rootLis, boundPort, err := BindPortWithFallback(cfg.GRPCPort, 10, cfg.ServerID)
	if err != nil {
		return fmt.Errorf("failed to bind port %s: %w", cfg.GRPCPort, err)
	}
	sm.boundPort = boundPort

	// 3. Initialize Single-Port Connection Multiplexer
	mux := NewUnifiedMultiplexer(rootLis, boundPort)
	sm.multiplexer = mux
	mux.Start()

	// 4. Wrap TLS listener with PausableListener for graceful pause/resume without socket teardown
	sm.pausableLis = NewPausableListener(mux.TLSListener())

	// 5. Configure Mutual TLS (mTLS) for gRPC
	certPool := x509.NewCertPool()
	certPool.AddCert(sm.certMgr.CACert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{sm.certMgr.ServerTLSCert},
		ClientCAs:    certPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(verifiedChains) == 0 || len(verifiedChains[0]) == 0 {
				return fmt.Errorf("no verified client certificate chain")
			}
			peerCert := verifiedChains[0][0]
			serialHex := NormalizeSerial(peerCert.SerialNumber)

			if sm.deviceMgr != nil {
				if sm.deviceMgr.IsRevoked(serialHex) {
					LogEvent(fmt.Sprintf("⛔ Access rejected: Device certificate %s has been revoked", serialHex))
					return fmt.Errorf("access revoked: device certificate %s is revoked by server", serialHex)
				}
				// Auto-register legacy paired clients or update last seen timestamp
				h := sha256.Sum256(peerCert.Raw)
				sm.deviceMgr.TouchOrAutoRegister(serialHex, hex.EncodeToString(h[:]))
			}
			return nil
		},
	}

	creds := credentials.NewTLS(tlsConfig)
	s := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterSnapHavenServer(s, &server{syncdir: cfg.SyncDirectory})

	sm.grpcServer = s
	sm.isRunning = true

	log.Printf("🟢 Unified SnapHaven server listening on :%d (Sync directory: %v)", boundPort, cfg.SyncDirectory)

	go func() {
		if err := s.Serve(sm.pausableLis); err != nil && err != grpc.ErrServerStopped {
			log.Printf("❌ gRPC server error: %v", err)
			sm.mu.Lock()
			sm.isRunning = false
			sm.mu.Unlock()
		}
	}()

	// 6. Broadcast via mDNS for zero-config discovery
	sm.mdnsBroadcaster = StartMDNSBroadcaster(cfg.ServerID, boundPort, GetVersion())

	return nil
}

func (sm *ServerManager) Stop() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.isRunning {
		return
	}

	log.Printf("⏸️ Pausing gRPC sync service...")
	if sm.pausableLis != nil {
		sm.pausableLis.SetPaused(true)
	}
	sm.isRunning = false
	log.Printf("⏸️ gRPC sync service paused (Web dashboard remains active).")
}

func (sm *ServerManager) Shutdown() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	log.Printf("🔴 Shutting down SnapHaven server...")
	if sm.mdnsBroadcaster != nil {
		sm.mdnsBroadcaster.Stop()
		sm.mdnsBroadcaster = nil
	}
	if sm.grpcServer != nil {
		sm.grpcServer.GracefulStop()
		sm.grpcServer = nil
	}
	if sm.multiplexer != nil {
		_ = sm.multiplexer.Close()
		sm.multiplexer = nil
		sm.pausableLis = nil
	}
	sm.isRunning = false
	log.Printf("🔴 Server shut down cleanly.")
}

func (sm *ServerManager) Restart() error {
	sm.mu.Lock()
	if sm.mdnsBroadcaster != nil {
		sm.mdnsBroadcaster.Stop()
		sm.mdnsBroadcaster = nil
	}
	if sm.grpcServer != nil {
		sm.grpcServer.Stop()
		sm.grpcServer = nil
	}
	if sm.multiplexer != nil {
		_ = sm.multiplexer.Close()
		sm.multiplexer = nil
		sm.pausableLis = nil
	}
	sm.isRunning = false
	sm.mu.Unlock()

	return sm.Start()
}

func (sm *ServerManager) AttachSetupServer(ss *SetupServer) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.setupServer = ss
}
