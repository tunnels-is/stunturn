// file: combined_server_final.go
package main

import (
	"encoding/json"
	"log"
	"net"
)

// Signal represents the data sent between client and server.
type Signal struct {
	Address string `json:"address"`
}

// Peer represents a waiting client, holding its address and its connection.
type Peer struct {
	PublicAddress string
	Conn          net.Conn
}

var peerChannel = make(chan Peer, 1)

func main() {
	listener, err := net.Listen("tcp", ":1000")
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
	defer listener.Close()

	log.Println("✅ Final TCP Server started on :1000")
	log.Println("   - Handles both STUN-like discovery and Signaling.")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		// Handle each client in a new goroutine.
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	// The remote address is our "STUN" discovery.
	clientAddr := conn.RemoteAddr().String()
	log.Printf("Client connected from %s", clientAddr)

	thisClient := Peer{
		PublicAddress: clientAddr,
		Conn:          conn,
	}

	select {
	case waitingPeer := <-peerChannel:
		// A peer was waiting. Pair them up.
		log.Printf("Pairing %s with %s", waitingPeer.PublicAddress, thisClient.PublicAddress)

		// Create encoders to send JSON over the raw TCP stream.
		encoderForWaitingPeer := json.NewEncoder(waitingPeer.Conn)
		encoderForThisClient := json.NewEncoder(thisClient.Conn)

		// Send peer info to each client.
		encoderForWaitingPeer.Encode(Signal{Address: thisClient.PublicAddress})
		encoderForThisClient.Encode(Signal{Address: waitingPeer.PublicAddress})

		// We are done with these connections.
		waitingPeer.Conn.Close()
		thisClient.Conn.Close()

	default:
		// No peer waiting. This client will wait.
		log.Printf("%s is waiting for a peer...", thisClient.PublicAddress)
		peerChannel <- thisClient
	}
}
