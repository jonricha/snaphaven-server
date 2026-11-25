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
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

type cachedFileHash struct {
	size        int64
	modTimeNano int64
	hash        string
}

var (
	fileHashCacheMu sync.RWMutex
	fileHashCache   = make(map[string]cachedFileHash)
	dateFilenameRe  = regexp.MustCompile(`(20\d{2})[-_]?(\d{2})[-_]?(\d{2})`)
)

const maxFileHashCacheEntries = 50000

type server struct {
	pb.UnimplementedSnapHavenServer
	syncdir     string
	folderOrg   string
	thumbMgr    *ThumbnailManager
	indexMu     sync.RWMutex
	indexLoaded bool
	byBaseName  map[string][]string
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

// Hash_file_sha256 streams a file from disk in 32 KB chunks without buffering the entire file
// in RAM, and caches results keyed by (path, size, modTimeUnixNano) for fast repeat checks.
func Hash_file_sha256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return "", fmt.Errorf("cannot hash directory: %s", filePath)
	}

	size := fi.Size()
	modNano := fi.ModTime().UnixNano()

	fileHashCacheMu.RLock()
	if cached, ok := fileHashCache[filePath]; ok && cached.size == size && cached.modTimeNano == modNano {
		fileHashCacheMu.RUnlock()
		return cached.hash, nil
	}
	fileHashCacheMu.RUnlock()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	digest := hex.EncodeToString(hasher.Sum(nil))

	fileHashCacheMu.Lock()
	if len(fileHashCache) >= maxFileHashCacheEntries {
		fileHashCache = make(map[string]cachedFileHash)
	}
	fileHashCache[filePath] = cachedFileHash{
		size:        size,
		modTimeNano: modNano,
		hash:        digest,
	}
	fileHashCacheMu.Unlock()

	return digest, nil
}

func Hash_bytes_sha256(b []byte) (string, error) {
	hasher := sha256.New()
	hasher.Write(b)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func isStandardDeviceMediaRoot(segment string) bool {
	switch strings.ToLower(strings.TrimSpace(segment)) {
	case "dcim", "camera", "pictures", "movies", "download", "downloads", "screenshots", "whatsapp images", "whatsapp video", "recents":
		return true
	}
	return false
}

func extractUserPrefix(clientRelPath string) string {
	cleanRel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(clientRelPath)), "/")
	parts := strings.Split(cleanRel, "/")
	if len(parts) >= 2 && parts[0] != "" && parts[0] != "." && parts[0] != ".." && !isStandardDeviceMediaRoot(parts[0]) {
		return parts[0]
	}
	return ""
}

func resolvePhotoTimestamp(baseName string, modTimeMs int64) time.Time {
	if modTimeMs > 0 {
		return time.UnixMilli(modTimeMs)
	}
	if m := dateFilenameRe.FindStringSubmatch(baseName); len(m) == 4 {
		if parsed, err := time.ParseInLocation("20060102", m[1]+m[2]+m[3], time.Local); err == nil {
			return parsed
		}
	}
	return time.Now()
}

// ResolveTargetUploadPath computes the destination file path on disk according to FolderOrganization.
func ResolveTargetUploadPath(syncdir, folderOrg, clientRelPath string, modTimeMs int64) string {
	org := NormalizeFolderOrganization(folderOrg)
	if org == FolderOrgDevice {
		return filepath.Join(syncdir, filepath.FromSlash(clientRelPath))
	}

	baseName := filepath.Base(filepath.FromSlash(clientRelPath))
	prefix := extractUserPrefix(clientRelPath)
	ts := resolvePhotoTimestamp(baseName, modTimeMs)

	var dateSubdir string
	if org == FolderOrgYearMonthDay {
		dateSubdir = ts.Format("2006/01/02")
	} else {
		dateSubdir = ts.Format("2006/2006-01")
	}

	var candidate string
	if prefix != "" {
		candidate = filepath.Join(syncdir, prefix, filepath.FromSlash(dateSubdir), baseName)
	} else {
		candidate = filepath.Join(syncdir, filepath.FromSlash(dateSubdir), baseName)
	}

	// Avoid clobbering an existing different file with the same filename in the same date bucket
	if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
		ext := filepath.Ext(baseName)
		stem := strings.TrimSuffix(baseName, ext)
		dir := filepath.Dir(candidate)
		for idx := 1; idx < 1000; idx++ {
			alt := filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, idx, ext))
			if _, statErr := os.Stat(alt); os.IsNotExist(statErr) {
				return alt
			}
		}
	}

	return candidate
}

func (s *server) ensureMediaIndex() {
	s.indexMu.RLock()
	if s.indexLoaded {
		s.indexMu.RUnlock()
		return
	}
	s.indexMu.RUnlock()

	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	if s.indexLoaded {
		return
	}

	idx := make(map[string][]string)
	if s.syncdir != "" {
		_ = filepath.Walk(s.syncdir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if path != s.syncdir && strings.HasPrefix(info.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			key := strings.ToLower(info.Name())
			idx[key] = append(idx[key], path)
			return nil
		})
	}
	s.byBaseName = idx
	s.indexLoaded = true
}

func (s *server) recordIndexedFile(fullPath string) {
	s.ensureMediaIndex()
	key := strings.ToLower(filepath.Base(fullPath))
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	for _, existing := range s.byBaseName[key] {
		if existing == fullPath {
			return
		}
	}
	s.byBaseName[key] = append(s.byBaseName[key], fullPath)
}

func (s *server) findExistingMediaPath(relPath string, expectedHash string) string {
	fullPath := filepath.Join(s.syncdir, filepath.FromSlash(relPath))
	if isSafePath(s.syncdir, fullPath) {
		if fi, err := os.Stat(fullPath); err == nil && !fi.IsDir() {
			if expectedHash == "" {
				return fullPath
			}
			if h, err := Hash_file_sha256(fullPath); err == nil && h == expectedHash {
				return fullPath
			}
		}
	}

	// Backward-compatibility path resolution (DCIM vs non-DCIM)
	cleanRel := filepath.ToSlash(filepath.Clean(relPath))
	parts := strings.Split(cleanRel, "/")
	if len(parts) >= 3 && parts[1] == "DCIM" {
		legacyPath := filepath.Join(s.syncdir, parts[0], filepath.Join(parts[2:]...))
		if isSafePath(s.syncdir, legacyPath) {
			if fi, err := os.Stat(legacyPath); err == nil && !fi.IsDir() {
				if expectedHash == "" {
					return legacyPath
				}
				if h, err := Hash_file_sha256(legacyPath); err == nil && h == expectedHash {
					return legacyPath
				}
			}
		}
	} else if len(parts) >= 2 && parts[1] != "DCIM" {
		dcimPath := filepath.Join(s.syncdir, parts[0], "DCIM", filepath.Join(parts[1:]...))
		if isSafePath(s.syncdir, dcimPath) {
			if fi, err := os.Stat(dcimPath); err == nil && !fi.IsDir() {
				if expectedHash == "" {
					return dcimPath
				}
				if h, err := Hash_file_sha256(dcimPath); err == nil && h == expectedHash {
					return dcimPath
				}
			}
		}
	}

	// Relocated date-folder lookup (e.g. YYYY/YYYY-MM or YYYY/MM/DD)
	baseKey := strings.ToLower(filepath.Base(cleanRel))
	if baseKey == "" || baseKey == "." || baseKey == "/" {
		return ""
	}
	// Match files whose base name starts with stem as well (for _1 collision suffixes)
	ext := strings.ToLower(filepath.Ext(baseKey))
	stem := strings.TrimSuffix(baseKey, ext)

	s.ensureMediaIndex()
	s.indexMu.RLock()
	var candidates []string
	candidates = append(candidates, s.byBaseName[baseKey]...)
	for k, list := range s.byBaseName {
		if k != baseKey && strings.HasPrefix(k, stem+"_") && strings.HasSuffix(k, ext) {
			candidates = append(candidates, list...)
		}
	}
	s.indexMu.RUnlock()

	prefix := extractUserPrefix(cleanRel)
	prefixRoot := ""
	if prefix != "" {
		prefixRoot = filepath.Join(s.syncdir, prefix) + string(filepath.Separator)
	}

	// Check same-prefix candidates first, then any remaining candidates
	for pass := 0; pass < 2; pass++ {
		for _, cand := range candidates {
			inPrefix := prefixRoot == "" || strings.HasPrefix(cand, prefixRoot)
			if (pass == 0 && !inPrefix) || (pass == 1 && inPrefix) {
				continue
			}
			fi, statErr := os.Stat(cand)
			if statErr != nil || fi.IsDir() {
				continue
			}
			if expectedHash == "" {
				return cand
			}
			if h, err := Hash_file_sha256(cand); err == nil && h == expectedHash {
				return cand
			}
		}
	}

	return ""
}

func (s *server) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingReply, error) {
	LogEvent(fmt.Sprintf("📡 Ping received from client (version: %s)", req.GetClientVersion()))
	return &pb.PingReply{
		ServerVersion: GetFormattedVersion(),
		ServerTimeMs:  time.Now().UnixMilli(),
	}, nil
}

func (s *server) shouldSend(path string, remotehash string) bool {
	return s.findExistingMediaPath(path, remotehash) == ""
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
		existingPath := s.findExistingMediaPath(fileinfo.GetPath(), fileinfo.GetHash())
		shouldSend := (existingPath == "")
		LogEvent(fmt.Sprintf("🔍 Checking %v (shouldSend: %v)", fileinfo.GetPath(), shouldSend))

		needThumb := false
		if tm != nil {
			cleanPath := strings.TrimPrefix(filepath.FromSlash(fileinfo.GetPath()), string(filepath.Separator))
			fullPath := existingPath
			if fullPath == "" {
				fullPath = filepath.Join(s.syncdir, cleanPath)
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
				if fullpathfile != "" {
					s.recordIndexedFile(fullpathfile)
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
			fullpathfile = ResolveTargetUploadPath(s.syncdir, s.folderOrg, filename, modTimeMs)
			if !isSafePath(s.syncdir, fullpathfile) {
				return fmt.Errorf("invalid or unauthorized upload path")
			}
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
	fullPath := s.findExistingMediaPath(req.GetPath(), req.GetSha256())
	if fullPath == "" {
		fullPath = filepath.Join(s.syncdir, cleanPath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
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
	fullPath := s.findExistingMediaPath(req.GetPath(), req.GetSha256())
	if fullPath == "" {
		fullPath = filepath.Join(s.syncdir, cleanPath)
	}

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
		fullPath := s.findExistingMediaPath(item.GetPath(), item.GetSha256())
		if fullPath == "" && item.GetSha256() != "" {
			// Check if file exists at path/relocated path even if hash differs so Exists/SizeMatches report accurately
			fullPath = s.findExistingMediaPath(item.GetPath(), "")
		}
		var fi os.FileInfo
		var err error
		if fullPath != "" {
			fi, err = os.Stat(fullPath)
		} else {
			err = os.ErrNotExist
		}

		if err != nil || fi == nil || fi.IsDir() {
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
		GetCertificate: cm.GetCertificate,
		ClientCAs:      certPool,
		ClientAuth:     tls.RequireAndVerifyClientCert,
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
	kaep := keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second,
		PermitWithoutStream: true,
	}
	kasp := keepalive.ServerParameters{
		MaxConnectionIdle:     15 * time.Minute,
		Time:                  2 * time.Hour,
		Timeout:               20 * time.Second,
	}
	opts := []grpc.ServerOption{
		grpc.Creds(creds),
		grpc.KeepaliveEnforcementPolicy(kaep),
		grpc.KeepaliveParams(kasp),
		grpc.InitialWindowSize(4 * 1024 * 1024),
		grpc.InitialConnWindowSize(8 * 1024 * 1024),
	}
	s := grpc.NewServer(opts...)
	pb.RegisterSnapHavenServer(s, &server{syncdir: commonSyncDir})
	log.Printf("mTLS gRPC server listening at %v, serving directory: %v", lis.Addr(), commonSyncDir)
	return s, lis
}

func main() {
	if len(os.Args) > 1 {
		arg := strings.ToLower(os.Args[1])
		if arg == "open" || arg == "dashboard" || arg == "photos" || arg == "folder" || arg == "qr" || arg == "status" || arg == "--open" || arg == "-open" || arg == "--photos" || arg == "--folder" || arg == "help" || arg == "--help" || arg == "-h" {
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

	var syncDirFlag, portFlag, certDirFlag, folderOrgFlag string
	var headlessFlag bool
	flag.StringVar(&syncDirFlag, "dir", "", "Directory to sync files to")
	flag.StringVar(&portFlag, "port", "", "Port to use (default is :50005)")
	flag.StringVar(&certDirFlag, "certdir", "", "Directory storing certificates and CA")
	flag.StringVar(&folderOrgFlag, "folder-org", "", "Folder organization: device, year_month, or year_month_day")
	flag.BoolVar(&headlessFlag, "headless", false, "Run in headless mode without system tray")
	flag.Parse()

	if headlessFlag {
		_ = os.Setenv("SNAP_HEADLESS", "true")
	}

	// Environment variable overrides (useful for Docker & NAS deployments)
	if envDir := os.Getenv("SNAP_DIR"); envDir != "" && syncDirFlag == "" {
		syncDirFlag = envDir
	}
	if envPort := os.Getenv("SNAP_PORT"); envPort != "" && portFlag == "" {
		portFlag = envPort
	}
	if envFolderOrg := os.Getenv("SNAP_FOLDER_ORG"); envFolderOrg != "" && folderOrgFlag == "" {
		folderOrgFlag = envFolderOrg
	}

	if syncDirFlag != "" {
		configMgr.Config.SyncDirectory = syncDirFlag
	}
	if portFlag != "" {
		configMgr.Config.GRPCPort = portFlag
	}
	if certDirFlag != "" {
		configMgr.Config.CertDirectory = certDirFlag
	}
	if folderOrgFlag != "" {
		configMgr.Config.FolderOrganization = NormalizeFolderOrganization(folderOrgFlag)
	}

	if configMgr.Config.GRPCPort == "" {
		configMgr.Config.GRPCPort = ":50005"
	}
	configMgr.Save()

	if configMgr.Config.SyncDirectoryBookmark != "" {
		if resolved, bmErr := RestoreSecurityScopedBookmarkPlatform(configMgr.Config.SyncDirectoryBookmark); bmErr != nil {
			log.Printf("Notice: Could not restore security-scoped folder access: %v", bmErr)
		} else if resolved != "" {
			configMgr.Config.SyncDirectory = resolved
		}
	}

	absSyncDir, err := filepath.Abs(configMgr.Config.SyncDirectory)
	if err == nil {
		configMgr.Config.SyncDirectory = absSyncDir
	}

	log.Printf("📁 Sync Target Directory: %v", configMgr.Config.SyncDirectory)
	log.Printf("🔌 Preferred Port: %v", configMgr.Config.GRPCPort)
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

	// 5. Create Server Manager & Start Unified Multiplexed Server
	srvMgr := NewServerManager(configMgr, certMgr, deviceMgr)
	if err := srvMgr.Start(); err != nil {
		log.Fatalf("Fatal: Failed to start SnapHaven server: %v", err)
	}

	// 6. Initialize Update Manager & Start Auto-Check Ticker
	updaterMgr := NewUpdateManager("", "")
	updaterMgr.StartAutoCheckTicker(DefaultCheckInterval)

	// 7. Initialize Web Setup & Dashboard Server on the multiplexed HTTP listener
	setupServer, err := NewSetupServer(srvMgr.HTTPListener(), srvMgr.BoundPort(), certMgr, deviceMgr, configMgr, srvMgr, updaterMgr)
	if err != nil {
		log.Printf("Warning: Failed to initialize setup web server: %v", err)
	} else {
		srvMgr.AttachSetupServer(setupServer)
		setupServer.Start()
	}

	isHeadless := headlessFlag || os.Getenv("HEADLESS") == "true" || os.Getenv("SNAP_HEADLESS") == "true"
	if isHeadless {
		log.Printf("🛡️ Running in Headless / NAS mode.")
		PrintTerminalQRCode(setupServer.GetPairingJSON(), setupServer.ServerURL, certMgr.CAFingerprint)

		// Wait for SIGINT or SIGTERM for graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Printf("Received termination signal, shutting down gracefully...")
		srvMgr.Shutdown()
		os.Exit(0)
	}

	// 8. Launch System Tray Interface (runs event loop on main thread)
	tray := NewTrayApp(srvMgr, setupServer, updaterMgr)
	tray.Run()
}

