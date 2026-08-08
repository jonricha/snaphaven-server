package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	pb "github.com/jonricha/snaphaven-server/snaphaven"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type ServerManager struct {
	mu           sync.Mutex
	configMgr    *ConfigManager
	certMgr      *CertManager
	grpcServer   *grpc.Server
	grpcListener net.Listener
	setupServer  *SetupServer
	isRunning    bool
}

func NewServerManager(cm *ConfigManager, certMgr *CertManager) *ServerManager {
	return &ServerManager{
		configMgr: cm,
		certMgr:   certMgr,
	}
}

func (sm *ServerManager) IsRunning() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.isRunning
}

func (sm *ServerManager) Start() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.isRunning {
		return nil
	}

	cfg := sm.configMgr.Config
	lis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", cfg.GRPCPort, err)
	}

	certPool := x509.NewCertPool()
	certPool.AddCert(sm.certMgr.CACert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{sm.certMgr.ServerTLSCert},
		ClientCAs:    certPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	creds := credentials.NewTLS(tlsConfig)
	s := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterSnapHavenServer(s, &server{syncdir: cfg.SyncDirectory})

	sm.grpcServer = s
	sm.grpcListener = lis
	sm.isRunning = true

	log.Printf("🟢 mTLS gRPC server started on %v (Sync directory: %v)", lis.Addr(), cfg.SyncDirectory)

	go func() {
		if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			log.Printf("❌ gRPC server error: %v", err)
			sm.mu.Lock()
			sm.isRunning = false
			sm.mu.Unlock()
		}
	}()

	return nil
}

func (sm *ServerManager) Stop() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.isRunning {
		return
	}

	log.Printf("🔴 Stopping gRPC server...")
	if sm.grpcServer != nil {
		sm.grpcServer.GracefulStop()
	}
	sm.isRunning = false
	log.Printf("🔴 gRPC server stopped.")
}

func (sm *ServerManager) Restart() error {
	sm.Stop()
	return sm.Start()
}

func (sm *ServerManager) AttachSetupServer(ss *SetupServer) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.setupServer = ss
}
