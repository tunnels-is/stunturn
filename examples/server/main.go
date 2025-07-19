package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
)

type ClientHello struct {
	UUID       string `json:"uuid"`
	TargetIP   string `json:"target_ip"`
	Protocol   string `json:"protocol"`
	UDPAddress string `json:"address"`
}

type ServerResponse struct {
	Protocol    string `json:"protocol"`
	PeerAddress string `json:"peer_address"`
	Error       string `json:"error,omitempty"`
}

type PeerInfo struct {
	UUID        string
	TargetIP    string
	Protocol    string
	UDPAddress  string
	TCPPort     int
	Connection  net.Conn
}

var peers = make(map[string]*PeerInfo)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <port>")
		fmt.Println("Example: go run main.go 8080")
		os.Exit(1)
	}

	port := os.Args[1]
	addr := ":" + port

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal("Failed to listen:", err)
	}
	defer listener.Close()

	fmt.Printf("Signal server listening on %s\n", addr)
	fmt.Println("Waiting for peer connections...")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept connection:", err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	var hello ClientHello
	if err := json.NewDecoder(conn).Decode(&hello); err != nil {
		log.Println("Failed to decode hello:", err)
		return
	}

	fmt.Printf("Received connection from %s (UUID: %s, Target: %s, Protocol: %s)\n",
		conn.RemoteAddr(), hello.UUID, hello.TargetIP, hello.Protocol)

	// Store peer info
	tcpAddr := conn.RemoteAddr().(*net.TCPAddr)
	peer := &PeerInfo{
		UUID:       hello.UUID,
		TargetIP:   hello.TargetIP,
		Protocol:   hello.Protocol,
		UDPAddress: hello.UDPAddress,
		TCPPort:    tcpAddr.Port,
		Connection: conn,
	}

	// Look for target peer
	var targetPeer *PeerInfo
	for _, p := range peers {
		if p.UUID == hello.TargetIP || (hello.TargetIP == "" && p.TargetIP == hello.UUID) {
			targetPeer = p
			break
		}
	}

	if targetPeer == nil {
		// Store this peer and wait for target
		peers[hello.UUID] = peer
		fmt.Printf("Peer %s waiting for target %s\n", hello.UUID, hello.TargetIP)
		return
	}

	// Found target peer, facilitate connection
	fmt.Printf("Facilitating connection between %s and %s\n", hello.UUID, targetPeer.UUID)

	// Determine protocol (prefer UDP if both support it)
	protocol := "tcp"
	var peerAddr, targetAddr string

	if hello.Protocol == "udp" && targetPeer.UDPAddress != "" {
		protocol = "udp"
		peerAddr = hello.UDPAddress
		targetAddr = targetPeer.UDPAddress
	} else if hello.Protocol == "" && targetPeer.Protocol == "" {
		// Auto-detect: use UDP if both have UDP addresses
		if hello.UDPAddress != "" && targetPeer.UDPAddress != "" {
			protocol = "udp"
			peerAddr = hello.UDPAddress
			targetAddr = targetPeer.UDPAddress
		} else {
			protocol = "tcp"
			peerAddr = conn.RemoteAddr().String()
			targetAddr = targetPeer.Connection.RemoteAddr().String()
		}
	} else {
		protocol = "tcp"
		peerAddr = conn.RemoteAddr().String()
		targetAddr = targetPeer.Connection.RemoteAddr().String()
	}

	// Send responses to both peers
	resp1 := ServerResponse{
		Protocol:    protocol,
		PeerAddress: targetAddr,
	}

	resp2 := ServerResponse{
		Protocol:    protocol,
		PeerAddress: peerAddr,
	}

	json.NewEncoder(conn).Encode(resp1)
	json.NewEncoder(targetPeer.Connection).Encode(resp2)

	// Clean up
	delete(peers, targetPeer.UUID)
	fmt.Printf("Connection facilitated: %s <-> %s (%s)\n", hello.UUID, targetPeer.UUID, protocol)
}
