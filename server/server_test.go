package main

import (
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

	clientCertBundlePEM, err := cm.SignClientCSR(csrPEM)
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
	s, lis := RegisterServer(tempdir, port, cm)
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
	}, client, ctx
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
