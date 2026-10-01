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
	syncdir  string
	thumbMgr *ThumbnailManager
}

func (s *server) getThumbManager() *ThumbnailManager {
	if s.thumbMgr != nil {
		return s.thumbMgr
	}
	configPath, _ := GetDefaultConfigPath()
	tm, err := NewThumbnailManager(filepath.Dir(configPath))
	if err == nil {
		s.thumbMgr = tm
	}
	return s.thumbMgr
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
	fullPath := filepath.Join(s.syncdir, filepath.FromSlash(path))
	hash, err := Hash_file_sha256(fullPath)
	if err == nil && remotehash == hash {
		return false
	}

	// Backward-compatibility path resolution:
	// If path has DCIM, check without DCIM; if path doesn't have DCIM, check with DCIM.
	cleanRel := filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(cleanRel, "/")
	if len(parts) >= 3 && parts[1] == "DCIM" {
		// e.g. "phone/DCIM/Camera/IMG.jpg" -> "phone/Camera/IMG.jpg"
		legacyPath := filepath.Join(s.syncdir, parts[0], filepath.Join(parts[2:]...))
		legacyHash, err := Hash_file_sha256(legacyPath)
		if err == nil && remotehash == legacyHash {
			return false
		}
	} else if len(parts) >= 2 && parts[1] != "DCIM" {
		// e.g. "phone/Camera/IMG.jpg" -> "phone/DCIM/Camera/IMG.jpg"
		dcimPath := filepath.Join(s.syncdir, parts[0], "DCIM", filepath.Join(parts[1:]...))
		dcimHash, err := Hash_file_sha256(dcimPath)
		if err == nil && remotehash == dcimHash {
			return false
		}
	}

	// hash is different or file missing, so we need to send
	return true
}

func (s *server) SendFileInfo(stream pb.SnapHaven_SendFileInfoServer) error {
	tm := s.getThumbManager()
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

		needThumb := false
		if tm != nil {
			cleanPath := strings.TrimPrefix(filepath.FromSlash(fileinfo.GetPath()), string(filepath.Separator))
			fullPath := filepath.Join(s.syncdir, cleanPath)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				parts := strings.Split(cleanPath, string(filepath.Separator))
				if len(parts) > 1 {
					altPath := filepath.Join(s.syncdir, filepath.Join(parts[1:]...))
					if fi, err := os.Stat(altPath); err == nil && !fi.IsDir() {
						fullPath = altPath
					}
				}
			}
			needThumb = !tm.HasThumbnailForFile(fullPath, cleanPath, fileinfo.GetHash())
		}

		if err := stream.Send(&pb.FileInfoReply{
			Path:          fileinfo.GetPath(),
			Hash:          fileinfo.GetHash(),
			Shouldsend:    shouldSend,
			NeedThumbnail: needThumb,
		}); err != nil {
			return err
		}
	}
}

func (s *server) SendFiles(stream pb.SnapHaven_SendFilesServer) error {
	filename := ""
	fullpathfile := ""
	var modTimeMs int64
	var f *os.File
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	for {
		file, err := stream.Recv()
		if err == io.EOF {
			if f != nil {
				_ = f.Close()
				f = nil
				if modTimeMs > 0 && fullpathfile != "" {
					t := time.UnixMilli(modTimeMs)
					_ = os.Chtimes(fullpathfile, t, t)
				}
			}
			LogEvent(fmt.Sprintf("✅ Finished receiving %v", filename))
			return stream.SendAndClose(&pb.FileReply{Path: filename, Received: true})
		}
		if err != nil {
			LogEvent(fmt.Sprintf("⚠️ Stream closed: %v", err))
			return err
		}

		if file.GetModTimeMs() > 0 {
			modTimeMs = file.GetModTimeMs()
		}

		if f == nil {
			filename = file.GetPath()
			fullpathfile = filepath.Join(s.syncdir, filepath.FromSlash(filename))
			LogEvent(fmt.Sprintf("📥 Receiving file: %v -> %v", filename, fullpathfile))

			if err = os.MkdirAll(filepath.Dir(fullpathfile), 0755); err != nil {
				return err
			}

			f, err = os.OpenFile(fullpathfile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
		}

		if _, err = f.Write(file.GetContents()); err != nil {
			return err
		}
	}
}

func isSafePath(baseDir, targetPath string) bool {
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != "."
}

func isVideoExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".mov", ".mkv", ".webm", ".avi", ".3gp", ".ts":
		return true
	}
	return false
}

func isMediaExtension(ext string) bool {
	if isVideoExtension(ext) {
		return true
	}
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif", ".dng", ".bmp":
		return true
	}
	return false
}

func (s *server) ListRemoteFiles(ctx context.Context, req *pb.ListFilesRequest) (*pb.ListFilesReply, error) {
	prefix := req.GetPrefix()
	baseDir := s.syncdir
	if prefix != "" {
		baseDir = filepath.Join(s.syncdir, filepath.FromSlash(prefix))
	}

	targetDir := baseDir
	if req.GetFolder() != "" {
		targetDir = filepath.Join(baseDir, filepath.FromSlash(req.GetFolder()))
	}

	if fi, err := os.Stat(targetDir); err != nil || !fi.IsDir() {
		return &pb.ListFilesReply{Files: nil, Folders: nil}, nil
	}

	var files []*pb.RemoteFileItem
	foldersMap := make(map[string]bool)
	tm := s.getThumbManager()

	err := filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != targetDir && !req.GetRecursive() && req.GetFolder() != "" {
				return filepath.SkipDir
			}
			if path != baseDir {
				relFolder, err := filepath.Rel(baseDir, path)
				if err == nil && relFolder != "." {
					foldersMap[filepath.ToSlash(relFolder)] = true
				}
			}
			return nil
		}

		ext := filepath.Ext(info.Name())
		if !isMediaExtension(ext) {
			return nil
		}

		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			relPath = info.Name()
		}

		parentFolder := filepath.Base(filepath.Dir(path))
		if parentFolder == "." || parentFolder == filepath.Base(baseDir) {
			parentFolder = ""
		}

		hasThumb := false
		if tm != nil {
			hasThumb = tm.HasThumbnailForFile(path, relPath, "")
			if !hasThumb {
				switch strings.ToLower(ext) {
				case ".jpg", ".jpeg", ".png", ".gif":
					hasThumb = true
				}
			}
		}

		files = append(files, &pb.RemoteFileItem{
			Path:         filepath.ToSlash(relPath),
			Filename:     info.Name(),
			Size:         info.Size(),
			ModTimeMs:    info.ModTime().UnixMilli(),
			IsVideo:      isVideoExtension(ext),
			HasThumbnail: hasThumb,
			Folder:       parentFolder,
		})
		return nil
	})

	if err != nil {
		return nil, err
	}

	var folders []string
	for f := range foldersMap {
		folders = append(folders, f)
	}

	return &pb.ListFilesReply{
		Files:   files,
		Folders: folders,
	}, nil
}

func (s *server) GetThumbnail(ctx context.Context, req *pb.ThumbnailRequest) (*pb.ThumbnailReply, error) {
	tm := s.getThumbManager()
	if tm == nil {
		return &pb.ThumbnailReply{Found: false}, nil
	}

	cleanPath := strings.TrimPrefix(filepath.FromSlash(req.GetPath()), string(filepath.Separator))
	fullPath := filepath.Join(s.syncdir, cleanPath)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		// Try matching under any top-level device folder (e.g. s.syncdir/*/<cleanPath>)
		matches, _ := filepath.Glob(filepath.Join(s.syncdir, "*", cleanPath))
		if len(matches) > 0 {
			fullPath = matches[0]
		} else {
			parts := strings.Split(cleanPath, string(filepath.Separator))
			if len(parts) > 1 {
				altPath := filepath.Join(s.syncdir, filepath.Join(parts[1:]...))
				if fi, err := os.Stat(altPath); err == nil && !fi.IsDir() {
					fullPath = altPath
				}
			}
		}
	}

	if data, mime, ok := tm.GetThumbnailForFile(fullPath, cleanPath, req.GetSha256()); ok {
		LogEvent(fmt.Sprintf("🖼️ Served cached thumbnail: %v", req.GetPath()))
		return &pb.ThumbnailReply{
			Path:     req.GetPath(),
			Data:     data,
			MimeType: mime,
			Found:    true,
		}, nil
	}

	// Fallback: Generate thumbnail on-the-fly from local image if present
	if data, mime, ok := tm.GenerateLocalThumbnail(fullPath); ok {
		LogEvent(fmt.Sprintf("⚡ Generated local thumbnail on-the-fly: %v", req.GetPath()))
		return &pb.ThumbnailReply{
			Path:     req.GetPath(),
			Data:     data,
			MimeType: mime,
			Found:    true,
		}, nil
	}
	if alt := GetAltDCIMPath(fullPath); alt != "" {
		if data, mime, ok := tm.GenerateLocalThumbnail(alt); ok {
			LogEvent(fmt.Sprintf("⚡ Generated local thumbnail via alt DCIM path: %v", req.GetPath()))
			return &pb.ThumbnailReply{
				Path:     req.GetPath(),
				Data:     data,
				MimeType: mime,
				Found:    true,
			}, nil
		}
	}

	LogEvent(fmt.Sprintf("⚠️ Thumbnail not found: %v (fullPath: %v)", req.GetPath(), fullPath))
	return &pb.ThumbnailReply{Path: req.GetPath(), Found: false}, nil
}

func (s *server) UploadThumbnail(ctx context.Context, req *pb.UploadThumbnailRequest) (*pb.UploadThumbnailReply, error) {
	tm := s.getThumbManager()
	if tm == nil {
		return &pb.UploadThumbnailReply{Success: false}, nil
	}

	cleanPath := strings.TrimPrefix(filepath.FromSlash(req.GetPath()), string(filepath.Separator))
	fullPath := filepath.Join(s.syncdir, cleanPath)

	primaryKey := tm.GetCacheKey(fullPath, "")
	var altKey, shaKey string
	if req.GetSha256() != "" {
		shaKey = req.GetSha256()
	}
	if alt := GetAltDCIMPath(fullPath); alt != "" {
		altKey = tm.GetCacheKey(alt, "")
	}

	err := tm.SaveThumbnailWithKeys(req.GetData(), req.GetMimeType(), primaryKey, shaKey, altKey)
	if err == nil {
		LogEvent(fmt.Sprintf("📥 Saved uploaded thumbnail for %v (key: %v)", req.GetPath(), primaryKey))
	} else {
		LogEvent(fmt.Sprintf("⚠️ Failed to save thumbnail for %v: %v", req.GetPath(), err))
	}
	return &pb.UploadThumbnailReply{Success: err == nil}, err
}

func (s *server) StreamMedia(req *pb.MediaRangeRequest, stream pb.SnapHaven_StreamMediaServer) error {
	fullPath := filepath.Join(s.syncdir, filepath.FromSlash(req.GetPath()))
	if !isSafePath(s.syncdir, fullPath) {
		return fmt.Errorf("invalid or unauthorized file path")
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return fmt.Errorf("failed to open media file: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	totalSize := fi.Size()
	start := req.GetStartByte()
	if start < 0 || start >= totalSize {
		return nil
	}

	length := req.GetLength()
	if length <= 0 || start+length > totalSize {
		length = totalSize - start
	}

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	const chunkSize = 512 * 1024 // 512KB chunks for smooth streaming
	buf := make([]byte, chunkSize)
	remaining := length
	currentOffset := start

	for remaining > 0 {
		toRead := int64(chunkSize)
		if remaining < toRead {
			toRead = remaining
		}

		n, err := io.ReadFull(f, buf[:toRead])
		if n > 0 {
			if sendErr := stream.Send(&pb.MediaChunk{
				Offset:    currentOffset,
				Data:      buf[:n],
				TotalSize: totalSize,
			}); sendErr != nil {
				return sendErr
			}
			currentOffset += int64(n)
			remaining -= int64(n)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *server) DownloadFile(req *pb.DownloadFileRequest, stream pb.SnapHaven_DownloadFileServer) error {
	fullPath := filepath.Join(s.syncdir, filepath.FromSlash(req.GetPath()))
	if !isSafePath(s.syncdir, fullPath) {
		return fmt.Errorf("invalid or unauthorized file path")
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return fmt.Errorf("failed to open file for download: %w", err)
	}
	defer f.Close()

	const chunkSize = 64 * 1024 // 64KB chunks
	buf := make([]byte, chunkSize)

	var modTimeMs int64
	if fi, statErr := f.Stat(); statErr == nil {
		modTimeMs = fi.ModTime().UnixMilli()
	}

	for {
		n, err := f.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&pb.FileChunk{
				Path:      req.GetPath(),
				Contents:  buf[:n],
				ModTimeMs: modTimeMs,
			}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *server) VerifyFiles(ctx context.Context, req *pb.VerifyFilesRequest) (*pb.VerifyFilesReply, error) {
	var results []*pb.VerifiedResult

	for _, item := range req.GetItems() {
		fullPath := filepath.Join(s.syncdir, filepath.FromSlash(item.GetPath()))
		fi, err := os.Stat(fullPath)

		// If not found, check backward-compatibility alternate path (DCIM vs non-DCIM)
		if err != nil {
			cleanRel := filepath.ToSlash(filepath.Clean(item.GetPath()))
			parts := strings.Split(cleanRel, "/")
			if len(parts) >= 3 && parts[1] == "DCIM" {
				legacyPath := filepath.Join(s.syncdir, parts[0], filepath.Join(parts[2:]...))
				fi, err = os.Stat(legacyPath)
				if err == nil {
					fullPath = legacyPath
				}
			} else if len(parts) >= 2 && parts[1] != "DCIM" {
				dcimPath := filepath.Join(s.syncdir, parts[0], "DCIM", filepath.Join(parts[1:]...))
				fi, err = os.Stat(dcimPath)
				if err == nil {
					fullPath = dcimPath
				}
			}
		}

		if err != nil || fi.IsDir() {
			results = append(results, &pb.VerifiedResult{
				Path:        item.GetPath(),
				Exists:      false,
				SizeMatches: false,
				HashMatches: false,
			})
			continue
		}

		sizeMatches := (fi.Size() == item.GetSize())
		hashMatches := false
		if sizeMatches && item.GetSha256() != "" {
			h, err := Hash_file_sha256(fullPath)
			if err == nil && h == item.GetSha256() {
				hashMatches = true
			}
		} else if sizeMatches {
			hashMatches = true
		}

		results = append(results, &pb.VerifiedResult{
			Path:        item.GetPath(),
			Exists:      true,
			SizeMatches: sizeMatches,
			HashMatches: hashMatches,
		})
	}

	return &pb.VerifyFilesReply{Results: results}, nil
}

func RegisterServer(commonSyncDir string, port string, cm *CertManager, dm *DeviceManager) (*grpc.Server, net.Listener) {
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
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(verifiedChains) == 0 || len(verifiedChains[0]) == 0 {
				return fmt.Errorf("no verified client certificate chain")
			}
			peerCert := verifiedChains[0][0]
			serialHex := NormalizeSerial(peerCert.SerialNumber)

			if dm != nil {
				if dm.IsRevoked(serialHex) {
					LogEvent(fmt.Sprintf("⛔ Access rejected: Device certificate %s has been revoked", serialHex))
					return fmt.Errorf("access revoked: device certificate %s is revoked by server", serialHex)
				}
				h := sha256.Sum256(peerCert.Raw)
				dm.TouchOrAutoRegister(serialHex, hex.EncodeToString(h[:]))
			}
			return nil
		},
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

	// 1. Ensure only one instance of SnapHaven Server runs at a time
	_, isSingle := EnsureSingleInstance()
	if !isSingle {
		// When a duplicate instance is triggered (e.g. clicking Open in App Store / Start Menu),
		// launch or show the active server's web dashboard in the browser, then cleanly exit.
		if url := FindActiveSetupURL(); url != "" {
			OpenBrowser(url)
		}
		os.Exit(0)
	}

	// 2. Initialize Log Hub & Streamer
	configPath, _ := GetDefaultConfigPath()
	logFilePath := filepath.Join(filepath.Dir(configPath), "snaphaven.log")
	InitLogHub(logFilePath)

	log.Printf("==================================================")
	log.Printf("🚀 Starting SnapHaven Server Application %s...", GetFormattedVersion())
	log.Printf("==================================================")

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

	// 4. Initialize Device Manager
	deviceMgr, err := NewDeviceManager(filepath.Dir(configPath))
	if err != nil {
		log.Printf("Notice: Failed to initialize DeviceManager: %v", err)
	}

	// 5. Create Server Manager & Start gRPC Server
	srvMgr := NewServerManager(configMgr, certMgr, deviceMgr)
	if err := srvMgr.Start(); err != nil {
		log.Printf("Warning: Failed to auto-start gRPC server: %v", err)
	}

	// 6. Initialize Update Manager & Start Auto-Check Ticker
	updaterMgr := NewUpdateManager("", "")
	updaterMgr.StartAutoCheckTicker(DefaultCheckInterval)

	// 7. Initialize Web Setup & Dashboard Server
	setupServer, err := NewSetupServer(configMgr.Config.GRPCPort, certMgr, deviceMgr, configMgr, srvMgr, updaterMgr)
	if err != nil {
		log.Printf("Warning: Failed to initialize setup web server: %v", err)
	} else {
		srvMgr.AttachSetupServer(setupServer)
		setupServer.Start()
	}

	// 8. Launch System Tray Interface (runs event loop on main thread)
	tray := NewTrayApp(srvMgr, setupServer, updaterMgr)
	tray.Run()
}

