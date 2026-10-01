package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/jonricha/snaphaven-server/snaphaven"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func startClient(t *testing.T, port string, cm *CertManager) (*grpc.ClientConn, pb.SnapHavenClient, context.Context, context.CancelFunc) {
	// Create client CSR and ask CertManager to sign it for mTLS test
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}

	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "TestClient"},
	}
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, clientKey)
	if err != nil {
		t.Fatalf("failed to create CSR: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})

	clientCertBundlePEM, _, err := cm.SignClientCSR(csrPEM)
	if err != nil {
		t.Fatalf("failed to sign client CSR: %v", err)
	}

	clientKeyBytes, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatalf("failed to marshal client key: %v", err)
	}
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyBytes})

	tlsCert, err := tls.X509KeyPair(clientCertBundlePEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("failed to load client keypair: %v", err)
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(cm.CACert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		RootCAs:      caPool,
		ServerName:   "localhost",
	}

	creds := credentials.NewTLS(tlsConfig)
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(creds))
	conn, err := grpc.Dial("localhost"+port, opts...)
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}

	// Contact the server
	ctx, cancel := context.WithCancel(context.Background())
	return conn, pb.NewSnapHavenClient(conn), ctx, cancel
}

func setupTestCase(t *testing.T) (func(t *testing.T), pb.SnapHavenClient, context.Context) {
	teardown, client, ctx, _ := setupTestCaseWithDir(t)
	return teardown, client, ctx
}

func setupTestCaseWithDir(t *testing.T) (func(t *testing.T), pb.SnapHavenClient, context.Context, string) {
	t.Log("setupTestCase >>")
	tempdir, err := ioutil.TempDir("", "filesyncserver")
	if err != nil {
		t.Fatal(err)
	}

	certdir, err := ioutil.TempDir("", "filesync_test_certs")
	if err != nil {
		t.Fatal(err)
	}

	cm, err := NewCertManager(certdir)
	if err != nil {
		t.Fatal(err)
	}

	port := ":0"
	failurechannel := make(chan error, 1)
	dm, _ := NewDeviceManager(tempdir)
	s, lis := RegisterServer(tempdir, port, cm, dm)
	actualPort := fmt.Sprintf(":%d", lis.Addr().(*net.TCPAddr).Port)
	go func() {
		if err := s.Serve(lis); err != nil {
			failurechannel <- err
		}
		close(failurechannel)
	}()
	conn, client, ctx, cancel := startClient(t, actualPort, cm)
	select { // check if the server failed to start
	case err := <-failurechannel:
		t.Fatalf("failed to serve: %v", err)
	default:
		t.Log("Server up and running")
	}

	t.Log("setupTestCase <<")
	return func(t *testing.T) {
		t.Log("teardown >>")
		t.Log("cancel context")
		cancel()
		t.Log("close client connection")
		conn.Close()
		t.Log("gracefully stop the server")
		s.GracefulStop()
		os.RemoveAll(tempdir)
		os.RemoveAll(certdir)
		t.Log("teardown <<")
	}, client, ctx, tempdir
}

type FileInfoTestData struct {
	filename   string
	filehash   string
	shouldsend bool
}

func checkFiles(t *testing.T, client pb.SnapHavenClient, input []FileInfoTestData) {
	stream, err := client.SendFileInfo(context.Background())
	if err != nil {
		t.Fatalf("SendFileInfo failed with: %v", err)
	}
	for _, testdata := range input {
		if err := stream.Send(&pb.FileInfoRequest{Path: testdata.filename, Hash: testdata.filehash}); err != nil {
			t.Fatalf("Failed to send %v with error %v", testdata.filename, err)
		}
		in, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Failed to receive a FileInfoReply : %v", err)
		}
		if in.GetShouldsend() != testdata.shouldsend {
			t.Fatalf("Expected %v for should send", testdata.shouldsend)
		}
	}
	stream.CloseSend()
}

func TestSendFileInfo(t *testing.T) {
	teardown, client, _ := setupTestCase(t)
	defer teardown(t)
	input := []FileInfoTestData{
		{filename: "/Test1.txt", filehash: "doesntmatter", shouldsend: true},
		{filename: "/Test2.txt", filehash: "something", shouldsend: true},
	}
	checkFiles(t, client, input)
}

func TestSendFiles(t *testing.T) {
	test1file := "/Test1.txt"
	test1contents := "Test contents from my awesome file that's not a file"
	test1hash, err := Hash_bytes_sha256([]byte(test1contents))
	if err != nil {
		t.Fatal(err)
	}
	teardown, client, _ := setupTestCase(t)
	defer teardown(t)
	input := []FileInfoTestData{
		{filename: test1file, filehash: test1hash, shouldsend: true},
		{filename: "/Test2.txt", filehash: "something", shouldsend: true},
	}
	checkFiles(t, client, input)

	stream, err := client.SendFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stream.Send(&pb.FileChunk{Path: test1file, Contents: []byte(test1contents)})
	filereply, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}
	if filereply.GetPath() != test1file {
		t.Fatalf("Expected reply of: %v, but was actually: %v", test1file, filereply.GetPath())
	}
	stream.CloseSend()

	// now we should expect a check to show that we don't need the file anymore
	input[0].shouldsend = false
	checkFiles(t, client, input)
}

func TestSendFilesWithDir(t *testing.T) {
	test1file := "/subdir1/Test1.txt"
	test1contents := "Test contents from my awesome file that's not a file"
	test1hash, err := Hash_bytes_sha256([]byte(test1contents))
	if err != nil {
		t.Fatal(err)
	}
	teardown, client, _ := setupTestCase(t)
	defer teardown(t)
	input := []FileInfoTestData{
		{filename: test1file, filehash: test1hash, shouldsend: true},
		{filename: "/Test2.txt", filehash: "something", shouldsend: true},
	}
	checkFiles(t, client, input)

	stream, err := client.SendFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stream.Send(&pb.FileChunk{Path: test1file, Contents: []byte(test1contents)})
	filereply, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}
	if filereply.GetPath() != test1file {
		t.Fatalf("Expected reply of: %v, but was actually: %v", test1file, filereply.GetPath())
	}
	stream.CloseSend()

	// now we should expect a check to show that we don't need the file anymore
	input[0].shouldsend = false
	checkFiles(t, client, input)
}

func TestSendFilesPreservesModTime(t *testing.T) {
	testfile := "/preserved_time.jpg"
	testcontents := "Preserve my timestamp please!"
	// Target timestamp: 2021-05-15 14:30:00 UTC (1621089000000 ms)
	targetModTimeMs := int64(1621089000000)

	teardown, client, _, dir := setupTestCaseWithDir(t)
	defer teardown(t)

	stream, err := client.SendFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	err = stream.Send(&pb.FileChunk{
		Path:      testfile,
		Contents:  []byte(testcontents),
		ModTimeMs: targetModTimeMs,
	})
	if err != nil {
		t.Fatal(err)
	}

	filereply, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}
	if filereply.GetPath() != testfile {
		t.Fatalf("Expected reply %s, got %s", testfile, filereply.GetPath())
	}

	// Verify the file's modtime on disk matches the sent modtime
	savedFilePath := filepath.Join(dir, filepath.FromSlash(testfile))
	fi, err := os.Stat(savedFilePath)
	if err != nil {
		t.Fatalf("Failed to stat saved file: %v", err)
	}

	actualModTimeMs := fi.ModTime().UnixMilli()
	// Allow slight filesystem tolerance (up to 2 seconds for FAT/Windows filesystems)
	diff := actualModTimeMs - targetModTimeMs
	if diff < -2000 || diff > 2000 {
		t.Fatalf("Expected mod time ~%d, got %d (diff: %d ms)", targetModTimeMs, actualModTimeMs, diff)
	}

	// Test backward compatibility: old client sends chunk without ModTimeMs (0)
	testOldClientFile := "/old_client.jpg"
	stream2, err := client.SendFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = stream2.Send(&pb.FileChunk{
		Path:     testOldClientFile,
		Contents: []byte(testcontents),
		// ModTimeMs omitted -> 0
	})
	if err != nil {
		t.Fatal(err)
	}
	filereply2, err := stream2.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}
	if filereply2.GetPath() != testOldClientFile {
		t.Fatalf("Expected reply %s, got %s", testOldClientFile, filereply2.GetPath())
	}
	oldSavedPath := filepath.Join(dir, filepath.FromSlash(testOldClientFile))
	if _, err := os.Stat(oldSavedPath); err != nil {
		t.Fatalf("Old client file was not saved: %v", err)
	}
}

func TestPing(t *testing.T) {
	teardown, client, _ := setupTestCase(t)
	defer teardown(t)

	reply, err := client.Ping(context.Background(), &pb.PingRequest{ClientVersion: "1.0.0-test"})
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	if reply.GetServerVersion() == "" {
		t.Fatalf("Expected non-empty server version")
	}
	if reply.GetServerTimeMs() <= 0 {
		t.Fatalf("Expected positive server time, got %d", reply.GetServerTimeMs())
	}
}

func TestSendFilesMultiChunk(t *testing.T) {
	testFile := "/video_multichunk.bin"
	// Generate 100 chunks of 1KB each (100KB total) to thoroughly exercise chunk appending
	chunkSize := 1024
	numChunks := 100
	var fullContents []byte
	for i := 0; i < numChunks; i++ {
		chunk := bytes.Repeat([]byte{byte(i % 256)}, chunkSize)
		fullContents = append(fullContents, chunk...)
	}

	expectedHash, err := Hash_bytes_sha256(fullContents)
	if err != nil {
		t.Fatal(err)
	}

	teardown, client, _ := setupTestCase(t)
	defer teardown(t)

	// Pre-check: file should be needed
	input := []FileInfoTestData{
		{filename: testFile, filehash: expectedHash, shouldsend: true},
	}
	checkFiles(t, client, input)

	// Stream file across multiple chunks
	stream, err := client.SendFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < numChunks; i++ {
		chunk := fullContents[i*chunkSize : (i+1)*chunkSize]
		if err := stream.Send(&pb.FileChunk{Path: testFile, Contents: chunk}); err != nil {
			t.Fatalf("Failed to send chunk %d: %v", i, err)
		}
	}

	fileReply, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("Failed CloseAndRecv: %v", err)
	}
	if fileReply.GetPath() != testFile {
		t.Fatalf("Expected reply for %s, got %s", testFile, fileReply.GetPath())
	}
	if !fileReply.GetReceived() {
		t.Fatalf("Expected fileReply.Received to be true")
	}

	// Post-check: file should now exist on server with matching hash, so shouldsend is false
	input[0].shouldsend = false
	checkFiles(t, client, input)
}

func TestRemoteVault_ListAndDownload(t *testing.T) {
	teardown, client, _, serverDir := setupTestCaseWithDir(t)
	defer teardown(t)

	// Create test file structure in serverDir under "phone" prefix
	phoneDir := filepath.Join(serverDir, "phone", "Camera")
	if err := os.MkdirAll(phoneDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}

	testData := []byte("high-res photo content for testing")
	testFilePath := filepath.Join(phoneDir, "IMG_2026.jpg")
	if err := os.WriteFile(testFilePath, testData, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// 1. Test ListRemoteFiles
	listReply, err := client.ListRemoteFiles(context.Background(), &pb.ListFilesRequest{
		Prefix:    "phone",
		Folder:    "",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("ListRemoteFiles failed: %v", err)
	}
	if len(listReply.GetFiles()) != 1 {
		t.Fatalf("Expected 1 remote file, got %d", len(listReply.GetFiles()))
	}
	item := listReply.GetFiles()[0]
	if item.GetFilename() != "IMG_2026.jpg" {
		t.Errorf("Expected filename IMG_2026.jpg, got %s", item.GetFilename())
	}
	if item.GetSize() != int64(len(testData)) {
		t.Errorf("Expected size %d, got %d", len(testData), item.GetSize())
	}
	if item.GetIsVideo() {
		t.Errorf("Expected isVideo false for jpg")
	}

	// 2. Test DownloadFile
	downloadStream, err := client.DownloadFile(context.Background(), &pb.DownloadFileRequest{
		Path: "phone/Camera/IMG_2026.jpg",
	})
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	var downloaded bytes.Buffer
	for {
		chunk, err := downloadStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Download stream error: %v", err)
		}
		downloaded.Write(chunk.GetContents())
	}

	if !bytes.Equal(downloaded.Bytes(), testData) {
		t.Errorf("Downloaded content mismatch")
	}
}

func TestRemoteVault_Thumbnails(t *testing.T) {
	teardown, client, _ := setupTestCase(t)
	defer teardown(t)

	thumbData := []byte("fake_webp_bytes")
	path := "phone/Camera/IMG_TEST.jpg"
	sha := "abcdef123456"

	// 1. Upload thumbnail
	upReply, err := client.UploadThumbnail(context.Background(), &pb.UploadThumbnailRequest{
		Path:     path,
		Sha256:   sha,
		Data:     thumbData,
		MimeType: "image/webp",
	})
	if err != nil {
		t.Fatalf("UploadThumbnail failed: %v", err)
	}
	if !upReply.GetSuccess() {
		t.Errorf("Expected UploadThumbnail success")
	}

	// 2. Get thumbnail
	getReply, err := client.GetThumbnail(context.Background(), &pb.ThumbnailRequest{
		Path:   path,
		Sha256: sha,
	})
	if err != nil {
		t.Fatalf("GetThumbnail failed: %v", err)
	}
	if !getReply.GetFound() {
		t.Errorf("Expected thumbnail found")
	}
	if !bytes.Equal(getReply.GetData(), thumbData) {
		t.Errorf("Thumbnail data mismatch")
	}
}

func TestRemoteVault_StreamMedia(t *testing.T) {
	teardown, client, _, serverDir := setupTestCaseWithDir(t)
	defer teardown(t)

	// Create test video file
	videoDir := filepath.Join(serverDir, "phone", "Camera")
	_ = os.MkdirAll(videoDir, 0755)
	dummyVideo := make([]byte, 1024*1024) // 1MB
	for i := range dummyVideo {
		dummyVideo[i] = byte(i % 256)
	}
	videoPath := filepath.Join(videoDir, "test_video.mp4")
	_ = os.WriteFile(videoPath, dummyVideo, 0644)

	// Request range from 500,000 for 100,000 bytes
	rangeReq := &pb.MediaRangeRequest{
		Path:      "phone/Camera/test_video.mp4",
		StartByte: 500000,
		Length:    100000,
	}

	stream, err := client.StreamMedia(context.Background(), rangeReq)
	if err != nil {
		t.Fatalf("StreamMedia failed: %v", err)
	}

	var streamed bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("StreamMedia recv error: %v", err)
		}
		if chunk.GetTotalSize() != int64(len(dummyVideo)) {
			t.Errorf("Expected total size %d, got %d", len(dummyVideo), chunk.GetTotalSize())
		}
		streamed.Write(chunk.GetData())
	}

	expectedRange := dummyVideo[500000 : 500000+100000]
	if !bytes.Equal(streamed.Bytes(), expectedRange) {
		t.Errorf("Streamed byte range mismatch")
	}
}

func TestRemoteVault_VerifyFiles(t *testing.T) {
	teardown, client, _, serverDir := setupTestCaseWithDir(t)
	defer teardown(t)

	fileDir := filepath.Join(serverDir, "phone", "Camera")
	_ = os.MkdirAll(fileDir, 0755)
	fileData := []byte("verification test file data")
	filePath := filepath.Join(fileDir, "verify_me.jpg")
	_ = os.WriteFile(filePath, fileData, 0644)
	h, _ := Hash_bytes_sha256(fileData)

	verifyReq := &pb.VerifyFilesRequest{
		Items: []*pb.VerifyItem{
			{Path: "phone/Camera/verify_me.jpg", Size: int64(len(fileData)), Sha256: h},
			{Path: "phone/Camera/non_existent.jpg", Size: 1234, Sha256: "fake"},
		},
	}

	reply, err := client.VerifyFiles(context.Background(), verifyReq)
	if err != nil {
		t.Fatalf("VerifyFiles failed: %v", err)
	}
	if len(reply.GetResults()) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(reply.GetResults()))
	}

	r0 := reply.GetResults()[0]
	if !r0.GetExists() || !r0.GetSizeMatches() || !r0.GetHashMatches() {
		t.Errorf("Expected r0 all true, got exists=%v, size=%v, hash=%v", r0.GetExists(), r0.GetSizeMatches(), r0.GetHashMatches())
	}

	r1 := reply.GetResults()[1]
	if r1.GetExists() {
		t.Errorf("Expected r1 exists=false for non-existent file")
	}
}

func TestBackwardCompatiblePathResolution(t *testing.T) {
	teardown, client, _, serverDir := setupTestCaseWithDir(t)
	defer teardown(t)

	// File exists at legacy path "phone/Camera/legacy.jpg" (no DCIM)
	cameraDir := filepath.Join(serverDir, "phone", "Camera")
	_ = os.MkdirAll(cameraDir, 0755)
	fileData := []byte("legacy photo content")
	_ = os.WriteFile(filepath.Join(cameraDir, "legacy.jpg"), fileData, 0644)
	h, _ := Hash_bytes_sha256(fileData)

	// Check with new path "phone/DCIM/Camera/legacy.jpg" -> should recognize it via shouldSend fallback!
	stream, err := client.SendFileInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := stream.Send(&pb.FileInfoRequest{
		Path: "phone/DCIM/Camera/legacy.jpg",
		Hash: h,
	}); err != nil {
		t.Fatal(err)
	}
	_ = stream.CloseSend()

	reply, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if reply.GetShouldsend() {
		t.Errorf("Expected shouldSend=false via backward-compatibility DCIM fallback!")
	}
}
