package server

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
	UUID       string `json:"uuid"`
	TargetIP   string `json:"target_ip,omitempty"`
	Protocol   string `json:"protocol"`
	UDPAddress string `json:"address"`

	// server only
	ResponseChan  chan ClientResponse `json:"-"`
	PublicAddress string              `json:"-"`
}

type ClientResponse struct {
	Protocol    string `json:"protocol"`
	PeerAddress string `json:"peer_address,omitempty"`
	Error       string `json:"error,omitempty"`
}

func makePeeringKey(uuid, ip string) string {
	return uuid + ip
}

var waitingPeers sync.Map

func Start(address string) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
	defer listener.Close()
	go startUdpStunServer()

	for {
		conn, err := listener.Accept()
		if err != nil {
			// log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {

	var hello ClientHello
	if err := json.NewDecoder(conn).Decode(&hello); err != nil {
		_ = conn.Close()
		return
	}

	clientPublicAddr := conn.RemoteAddr().String()

	if hello.TargetIP == "" {
		server(conn, hello, clientPublicAddr)
	} else {
		client(conn, hello, clientPublicAddr)
	}
}

func client(conn net.Conn, hello ClientHello, publicAddr string) {
	defer conn.Close()
	defer waitingPeers.Delete(hello.UUID)

	if hello.Protocol == "udp" {
		hello.PublicAddress = hello.UDPAddress
	} else {
		hello.PublicAddress = publicAddr
	}
	hello.ResponseChan = make(chan ClientResponse)

	key := makePeeringKey(hello.UUID, hello.TargetIP)
	fmt.Println("CLIENT KEY:", key)
	if _, loaded := waitingPeers.LoadOrStore(key, hello); loaded {
		json.NewEncoder(conn).Encode(ClientResponse{Error: "UUID already in use"})
		return
	}

	log.Printf("Receiver registered with UUID %s. Waiting for initiator...", hello.UUID)

	select {
	case resp := <-hello.ResponseChan:
		if err := json.NewEncoder(conn).Encode(resp); err != nil {
			json.NewEncoder(conn).Encode(ClientResponse{Error: "Encoding error"})
		} else {
			log.Printf("Successfully sent peer info to receiver %s", hello.UUID)
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

	waitingPeer := value.(ClientHello)

	fmt.Println("SERVER!")
	if waitingPeer.Protocol == "udp" {
		waitingPeer.ResponseChan <- ClientResponse{PeerAddress: hello.UDPAddress, Protocol: waitingPeer.Protocol}
	} else {
		waitingPeer.ResponseChan <- ClientResponse{PeerAddress: publicAddr, Protocol: waitingPeer.Protocol}
	}

	json.NewEncoder(conn).Encode(ClientResponse{PeerAddress: waitingPeer.PublicAddress, Protocol: waitingPeer.Protocol})
	fmt.Println("DONE!")
}

func startUdpStunServer() {
	addr, err := net.ResolveUDPAddr("udp", ":1000")
	if err != nil {
		log.Fatalf("UDP: Failed to resolve address: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("UDP: Failed to listen: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	for {
		_, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("UDP: Error reading: %v", err)
			continue
		}

		conn.WriteToUDP([]byte(remoteAddr.String()), remoteAddr)
	}
}
