package main

import (
	"context"
	"io"
	"io/ioutil"
	"log"
	"os"
	"testing"

	pb "filesync/server/filesync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func startClient(t *testing.T, port string) (*grpc.ClientConn, pb.FileSyncClient, context.Context, context.CancelFunc) {
	var opts []grpc.DialOption
	creds, err := credentials.NewClientTLSFromFile("certs/ca_cert.pem", "x.test.example.com")
	if err != nil {
		t.Fatalf("failed to create TLS credentials: %v", err)
	}
	opts = append(opts, grpc.WithTransportCredentials(creds))
	conn, err := grpc.Dial("localhost"+port, opts...)
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}

	// Contact the server
	ctx, cancel := context.WithCancel(context.Background())
	return conn, pb.NewFileSyncClient(conn), ctx, cancel

}

func setupTestCase(t *testing.T) (func(t *testing.T), pb.FileSyncClient, context.Context) {
	t.Log("setupTestCase >>")
	tempdir, err := ioutil.TempDir("", "filesyncserver")
	if err != nil {
		t.Fatal(err)
	}

	port := ":50010"
	failurechannel := make(chan error, 1)
	s, lis := RegisterServer(tempdir, port)
	go func() {
		if err := s.Serve(lis); err != nil {
			failurechannel <- err
		}
		close(failurechannel)
	}()
	select { // check if the server failed to start
	case err := <-failurechannel:
		t.Fatalf("failed to serve: %v", err)
	default:
		t.Log("Server up and running")
	}
	conn, client, ctx, cancel := startClient(t, port)

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
		t.Log("teardown <<")
	}, client, ctx
}

type FileInfoTestData struct {
	filename   string
	filehash   string
	shouldsend bool
}

func checkFiles(t *testing.T, client pb.FileSyncClient, input []FileInfoTestData) {
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
