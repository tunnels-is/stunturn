// file: combined_server_roles.go
package main

import (
	"encoding/json"
	"log"
	"net"
	"sync"
)

// ClientHello is the initial message a client sends to define its role.
type ClientHello struct {
	Role     string `json:"role"`      // "initiator" or "receiver"
	UUID     string `json:"uuid"`      // Shared secret token
	TargetIP string `json:"target_ip"` // For initiator: the IP of the receiver it wants
}

// ServerResponse is the message the server sends back with the peer's address.
type ServerResponse struct {
	PeerAddress string `json:"peer_address"`
	Error       string `json:"error,omitempty"`
}

// Peer represents a waiting receiver client.
type Peer struct {
	PublicAddress string
	Conn          net.Conn
}

// Use a map to store waiting receivers, keyed by UUID.
var waitingPeers = make(map[string]Peer)

// A mutex is required to safely access the map from multiple goroutines.
var peerLock sync.Mutex

func main() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
	defer listener.Close()

	log.Println("✅ Role-based TCP Server started on :8080")

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
	defer conn.Close()
	decoder := json.NewDecoder(conn)

	var hello ClientHello
	if err := decoder.Decode(&hello); err != nil {
		log.Printf("Failed to decode client hello from %s: %v", conn.RemoteAddr(), err)
		return
	}

	clientPublicAddr := conn.RemoteAddr().String()

	switch hello.Role {
	case "receiver":
		handleReceiver(conn, hello, clientPublicAddr)
	case "initiator":
		handleInitiator(conn, hello, clientPublicAddr)
	default:
		log.Printf("Invalid role '%s' from client %s", hello.Role, clientPublicAddr)
	}
}

func handleReceiver(conn net.Conn, hello ClientHello, publicAddr string) {
	peerLock.Lock()
	if _, exists := waitingPeers[hello.UUID]; exists {
		// It's ambiguous what to do here. For simplicity, we reject the new one.
		peerLock.Unlock()
		log.Printf("Receiver with UUID %s already waiting. Rejecting new connection from %s.", hello.UUID, publicAddr)
		json.NewEncoder(conn).Encode(ServerResponse{Error: "UUID already in use"})
		return
	}

	log.Printf("Receiver registered with UUID %s from %s. Waiting for initiator.", hello.UUID, publicAddr)
	waitingPeers[hello.UUID] = Peer{
		PublicAddress: publicAddr,
		Conn:          conn,
	}
	peerLock.Unlock()

	// The connection is now held open, waiting for the initiator.
	// If the initiator never comes, this goroutine will exit when the client times out and disconnects.
}

func handleInitiator(conn net.Conn, hello ClientHello, publicAddr string) {
	peerLock.Lock()
	waitingPeer, ok := waitingPeers[hello.UUID]
	if !ok {
		peerLock.Unlock()
		log.Printf("Initiator from %s requested UUID %s, but no receiver was found.", publicAddr, hello.UUID)
		json.NewEncoder(conn).Encode(ServerResponse{Error: "Receiver with that UUID not found"})
		return
	}

	// Receiver found, remove it from the map so it can't be used again.
	delete(waitingPeers, hello.UUID)
	peerLock.Unlock()

	// --- Security Check ---
	// Does the waiting peer's actual public IP match what the initiator requested?
	if waitingPeer.PublicAddress != hello.TargetIP {
		log.Printf("SECURITY VIOLATION: Initiator from %s for UUID %s requested target IP %s, but receiver's actual IP is %s.",
			publicAddr, hello.UUID, hello.TargetIP, waitingPeer.PublicAddress)

		// Inform both parties of the failure and close connections.
		json.NewEncoder(conn).Encode(ServerResponse{Error: "Target IP mismatch"})
		json.NewEncoder(waitingPeer.Conn).Encode(ServerResponse{Error: "An initiator attempted to connect with an incorrect target IP"})
		waitingPeer.Conn.Close()
		return
	}

	log.Printf("Match success for UUID %s! Peering %s (receiver) with %s (initiator).",
		hello.UUID, waitingPeer.PublicAddress, publicAddr)

	// Send initiator's address to the receiver
	json.NewEncoder(waitingPeer.Conn).Encode(ServerResponse{PeerAddress: publicAddr})
	// Send receiver's address to the initiator
	json.NewEncoder(conn).Encode(ServerResponse{PeerAddress: waitingPeer.PublicAddress})

	// The job is done, close the signaling connections.
	waitingPeer.Conn.Close()
}
