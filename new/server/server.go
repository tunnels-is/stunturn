// file: combined_server_robust.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type ClientHello struct {
	Role     string `json:"role"`
	UUID     string `json:"uuid"`
	TargetIP string `json:"target_ip,omitempty"`
}

type ClientResponse struct {
	PeerAddress string `json:"peer_address,omitempty"`
	Error       string `json:"error,omitempty"`
}

type Peer struct {
	WaitingIP     string
	PublicIP      string
	PublicAddress string
	ResponseChan  chan ClientResponse
}

func makePeeringKey(uuid, ip string) string {
	return uuid + ip
}

var waitingPeers sync.Map

func main() {
	listener, err := net.Listen("tcp", ":1000")
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {

	var hello ClientHello
	if err := json.NewDecoder(conn).Decode(&hello); err != nil {
		log.Printf("Failed to decode client hello from %s: %v", conn.RemoteAddr(), err)
		_ = conn.Close()
		return
	}

	clientPublicAddr := conn.RemoteAddr().String()

	switch hello.Role {
	case "client":
		client(conn, hello, clientPublicAddr)
	case "server":
		server(conn, hello, clientPublicAddr)
	default:
		json.NewEncoder(conn).Encode(ClientResponse{Error: "Invalid role specified"})
		_ = conn.Close()
	}
}

func client(conn net.Conn, hello ClientHello, publicAddr string) {
	defer conn.Close()
	defer waitingPeers.Delete(hello.UUID)

	responseChan := make(chan ClientResponse)
	newPeer := Peer{
		WaitingIP:     hello.TargetIP,
		PublicAddress: publicAddr,
		ResponseChan:  responseChan,
	}

	key := makePeeringKey(hello.UUID, hello.TargetIP)
	fmt.Println("CLIENT KEY:", key)
	if _, loaded := waitingPeers.LoadOrStore(key, newPeer); loaded {
		json.NewEncoder(conn).Encode(ClientResponse{Error: "UUID already in use"})
		return
	}

	log.Printf("Receiver registered with UUID %s. Waiting for initiator...", hello.UUID)

	select {
	case resp := <-responseChan:
		if err := json.NewEncoder(conn).Encode(resp); err != nil {
			json.NewEncoder(conn).Encode(ClientResponse{Error: "Encoding error"})
		} else {
			log.Printf("Successfully sent peer info to receiver %s", publicAddr)
		}
	case <-time.After(20 * time.Second):
		json.NewEncoder(conn).Encode(ClientResponse{Error: "Timed out waiting for an initiator"})
	}
}

func server(conn net.Conn, hello ClientHello, publicAddr string) {
	defer conn.Close()

	sp := strings.Split(publicAddr, ":")

	key := makePeeringKey(hello.UUID, sp[0])
	fmt.Println("SERVER KEY:", key)
	value, ok := waitingPeers.LoadAndDelete(key)
	if !ok {
		json.NewEncoder(conn).Encode(ClientResponse{Error: "Receiver with that UUID not found"})
		return
	}

	waitingPeer := value.(Peer)

	fmt.Println("SERVER PEER!")
	waitingPeer.ResponseChan <- ClientResponse{PeerAddress: publicAddr}
	json.NewEncoder(conn).Encode(ClientResponse{PeerAddress: waitingPeer.PublicAddress})
}
