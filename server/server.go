package main

import (
	"crypto/sha256"
	"encoding/hex"
	pb "filesync/server/filesync"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedFileSyncServer
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

func (s *server) shouldSend(path string, remotehash string) bool {
	// var polynomial uint32
	// polynomial = 0xFFFF3333
	// hash, err := Hash_file_crc32(filepath.FromSlash(s.syncdir+path), polynomial)
	hash, err := Hash_file_sha256(filepath.FromSlash(s.syncdir + path))
	if err != nil || remotehash != hash {
		return true
	}
	// hash is the same, so we don't need to send
	return false
}

func (s *server) SendFileInfo(stream pb.FileSync_SendFileInfoServer) error {
	for {
		fileinfo, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		shouldSend := s.shouldSend(fileinfo.GetPath(), fileinfo.GetHash())
		log.Printf("Checking %v, shouldSend: %v", fileinfo.GetPath(), shouldSend)
		if err := stream.Send(&pb.FileInfoReply{Path: fileinfo.GetPath(), Hash: fileinfo.GetHash(), Shouldsend: shouldSend}); err != nil {
			return err
		}
	}
}

func (s *server) SendFiles(stream pb.FileSync_SendFilesServer) error {
	filename := ""
	first := true
	for {
		file, err := stream.Recv()
		if err == io.EOF {
			log.Printf("Finished receiving %v", filename)
			return stream.SendAndClose(&pb.FileReply{Path: filename, Received: true})
		}
		if err != nil {
			log.Printf("Stream was closed? %v", err)
			return err
		}
		fileopenmask := os.O_WRONLY | os.O_CREATE
		if first {
			filename = file.GetPath()
			log.Printf("Receiving file: %v", filename)
			first = false
			fileopenmask |= os.O_TRUNC
		} else {
			// only append on subsequent chunks
			fileopenmask |= os.O_APPEND
		}
		fullpathfile := s.syncdir + filepath.FromSlash(file.GetPath())
		if err = os.MkdirAll(filepath.Dir(fullpathfile), 0666); err != nil {
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

func RegisterServer(commonSyncDir string, port string) (*grpc.Server, net.Listener) {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterFileSyncServer(s, &server{syncdir: commonSyncDir})
	log.Printf("server listening at %v, serving %v", lis.Addr(), commonSyncDir)
	return s, lis
}

func main() {
	homedir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	syncdirPtr := flag.String("dir", filepath.FromSlash(homedir+"/filesync"), "Directory to sync files to")
	portPtr := flag.String("port", ":50005", "Port to use (default is recommended)")
	flag.Parse()
	s, lis := RegisterServer(*syncdirPtr, *portPtr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
