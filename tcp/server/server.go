// file: combined_server_final.go
package main

import (
	"encoding/json"
	"log"
	"net"
)

type Signal struct {
	Address string `json:"address"`
}

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
	clientAddr := conn.RemoteAddr().String()

	thisClient := Peer{
		PublicAddress: clientAddr,
		Conn:          conn,
	}

	select {
	case waitingPeer := <-peerChannel:
		encoderForWaitingPeer := json.NewEncoder(waitingPeer.Conn)
		encoderForThisClient := json.NewEncoder(thisClient.Conn)
		encoderForWaitingPeer.Encode(Signal{Address: thisClient.PublicAddress})
		encoderForThisClient.Encode(Signal{Address: waitingPeer.PublicAddress})
		waitingPeer.Conn.Close()
		thisClient.Conn.Close()
	default:
		log.Printf("client waiting for peer")
		peerChannel <- thisClient
	}
}
