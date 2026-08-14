package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	pb "github.com/jonricha/snaphaven-server/snaphaven"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type server struct {
	pb.UnimplementedSnapHavenServer
	syncdir string
}

func Hash_file_sha256(filePath string) (string, error) {
	s, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return Hash_bytes_sha256(s)
}

func Hash_bytes_sha256(b []byte) (string, error) {
	hasher := sha256.New()
	hasher.Write(b)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (s *server) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingReply, error) {
	LogEvent(fmt.Sprintf("📡 Ping received from client (version: %s)", req.GetClientVersion()))
	return &pb.PingReply{
		ServerVersion: GetFormattedVersion(),
		ServerTimeMs:  time.Now().UnixMilli(),
	}, nil
}

func (s *server) shouldSend(path string, remotehash string) bool {
	hash, err := Hash_file_sha256(filepath.Join(s.syncdir, filepath.FromSlash(path)))
	if err != nil || remotehash != hash {
		return true
	}
	// hash is the same, so we don't need to send
	return false
}

func (s *server) SendFileInfo(stream pb.SnapHaven_SendFileInfoServer) error {
	for {
		fileinfo, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		shouldSend := s.shouldSend(fileinfo.GetPath(), fileinfo.GetHash())
		LogEvent(fmt.Sprintf("🔍 Checking %v (shouldSend: %v)", fileinfo.GetPath(), shouldSend))
		if err := stream.Send(&pb.FileInfoReply{Path: fileinfo.GetPath(), Hash: fileinfo.GetHash(), Shouldsend: shouldSend}); err != nil {
			return err
		}
	}
}

func (s *server) SendFiles(stream pb.SnapHaven_SendFilesServer) error {
	filename := ""
	first := true
	for {
		file, err := stream.Recv()
		if err == io.EOF {
			LogEvent(fmt.Sprintf("✅ Finished receiving %v", filename))
			return stream.SendAndClose(&pb.FileReply{Path: filename, Received: true})
		}
		if err != nil {
			LogEvent(fmt.Sprintf("⚠️ Stream closed: %v", err))
			return err
		}
		fileopenmask := os.O_WRONLY | os.O_CREATE
		fullpathfile := filepath.Join(s.syncdir, filepath.FromSlash(file.GetPath()))
		if first {
			filename = file.GetPath()
			LogEvent(fmt.Sprintf("📥 Receiving file: %v -> %v", filename, fullpathfile))
			first = false
			fileopenmask |= os.O_TRUNC
		} else {
			// only append on subsequent chunks
			fileopenmask |= os.O_APPEND
		}
		if err = os.MkdirAll(filepath.Dir(fullpathfile), 0755); err != nil {
			return err
		}

		// now append to the file
		f, err := os.OpenFile(fullpathfile, fileopenmask, 0644)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err = f.Write(file.GetContents()); err != nil {
			return err
		}
	}
}

func RegisterServer(commonSyncDir string, port string, cm *CertManager) (*grpc.Server, net.Listener) {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create mTLS credentials:
	// 1. Require client certificates
	// 2. Trust client certificates signed by our local Root CA
	certPool := x509.NewCertPool()
	certPool.AddCert(cm.CACert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cm.ServerTLSCert},
		ClientCAs:    certPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	creds := credentials.NewTLS(tlsConfig)
	var opts []grpc.ServerOption
	opts = []grpc.ServerOption{grpc.Creds(creds)}
	s := grpc.NewServer(opts...)
	pb.RegisterSnapHavenServer(s, &server{syncdir: commonSyncDir})
	log.Printf("mTLS gRPC server listening at %v, serving directory: %v", lis.Addr(), commonSyncDir)
	return s, lis
}

func main() {
	if len(os.Args) > 1 {
		arg := strings.ToLower(os.Args[1])
		if arg == "open" || arg == "dashboard" || arg == "qr" || arg == "status" || arg == "--open" || arg == "-open" || arg == "help" || arg == "--help" || arg == "-h" {
			HandleCLICommand(arg)
			return
		}
	}

	// 1. Initialize Log Hub & Streamer
	configPath, _ := GetDefaultConfigPath()
	logFilePath := filepath.Join(filepath.Dir(configPath), "snaphaven.log")
	InitLogHub(logFilePath)

	log.Printf("==================================================")
	log.Printf("🚀 Starting SnapHaven Server Application %s...", GetFormattedVersion())
	log.Printf("==================================================")

	// Ensure only one instance of SnapHaven Server runs at a time
	_, isSingle := EnsureSingleInstance()
	if !isSingle {
		log.Printf("⚠️ Another instance of SnapHaven Server is already running. Exiting duplicate process.")
		os.Exit(0)
	}

	// 2. Load / Create Configuration
	configMgr, err := NewConfigManager("")
	if err != nil {
		log.Fatalf("Failed to initialize configuration manager: %v", err)
	}

	var syncDirFlag, portFlag, certDirFlag string
	flag.StringVar(&syncDirFlag, "dir", "", "Directory to sync files to")
	flag.StringVar(&portFlag, "port", "", "Port to use (default is :50005)")
	flag.StringVar(&certDirFlag, "certdir", "", "Directory storing certificates and CA")
	flag.Parse()

	if syncDirFlag != "" {
		configMgr.Config.SyncDirectory = syncDirFlag
	}
	if portFlag != "" {
		configMgr.Config.GRPCPort = portFlag
	}
	if certDirFlag != "" {
		configMgr.Config.CertDirectory = certDirFlag
	}

	if configMgr.Config.GRPCPort == "" {
		configMgr.Config.GRPCPort = ":50005"
	}
	configMgr.Save()

	absSyncDir, err := filepath.Abs(configMgr.Config.SyncDirectory)
	if err == nil {
		configMgr.Config.SyncDirectory = absSyncDir
	}

	log.Printf("📁 Sync Target Directory: %v", configMgr.Config.SyncDirectory)
	log.Printf("🔌 gRPC Port: %v", configMgr.Config.GRPCPort)
	log.Printf("🏷️ Server Version: %v", GetFormattedVersion())

	// 3. Initialize Certificate Manager
	certMgr, err := NewCertManager(configMgr.Config.CertDirectory)
	if err != nil {
		log.Fatalf("Failed to initialize CertManager: %v", err)
	}

	// 4. Create Server Manager & Start gRPC Server
	srvMgr := NewServerManager(configMgr, certMgr)
	if err := srvMgr.Start(); err != nil {
		log.Printf("Warning: Failed to auto-start gRPC server: %v", err)
	}

	// 5. Initialize Update Manager & Start Auto-Check Ticker
	updaterMgr := NewUpdateManager("", "")
	updaterMgr.StartAutoCheckTicker(DefaultCheckInterval)

	// 6. Initialize Web Setup & Dashboard Server
	setupServer, err := NewSetupServer(configMgr.Config.GRPCPort, certMgr, configMgr, srvMgr, updaterMgr)
	if err != nil {
		log.Printf("Warning: Failed to initialize setup web server: %v", err)
	} else {
		srvMgr.AttachSetupServer(setupServer)
		setupServer.Start()
	}

	// 7. Launch System Tray Interface (runs event loop on main thread)
	tray := NewTrayApp(srvMgr, setupServer, updaterMgr)
	tray.Run()
}

