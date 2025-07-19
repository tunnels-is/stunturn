package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/tunnels-is/stunturn/client"
)

func main() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: go run main.go <signal_server> <my_uuid> <target_uuid> <protocol>")
		fmt.Println("Example: go run main.go localhost:8080 client1 client2 tcp")
		fmt.Println("Example: go run main.go localhost:8080 client1 client2 udp")
		fmt.Println("Example: go run main.go localhost:8080 client1 client2 auto")
		os.Exit(1)
	}

	signalServer := os.Args[1]
	myUUID := os.Args[2]
	targetUUID := os.Args[3]
	protocol := os.Args[4]

	fmt.Printf("Starting client: %s -> %s via %s (%s)\n", myUUID, targetUUID, signalServer, protocol)

	var conn net.Conn
	var udpConn *net.UDPConn
	var err error

	switch protocol {
	case "tcp":
		conn, err = establishTCPConnection(signalServer, myUUID, targetUUID)
		if err != nil {
			log.Fatal("Failed to establish TCP connection:", err)
		}
		fmt.Println("TCP connection established!")
		startTCPChat(conn, myUUID)

	case "udp":
		udpConn, err = establishUDPConnection(signalServer, myUUID, targetUUID)
		if err != nil {
			log.Fatal("Failed to establish UDP connection:", err)
		}
		fmt.Println("UDP connection established!")
		startUDPChat(udpConn, myUUID)

	case "auto":
		conn, udpConn, protocol, err = establishAutoConnection(signalServer, myUUID, targetUUID)
		if err != nil {
			log.Fatal("Failed to establish auto connection:", err)
		}
		
		if protocol == "udp" {
			fmt.Println("UDP connection established!")
			startUDPChat(udpConn, myUUID)
		} else {
			fmt.Println("TCP connection established!")
			startTCPChat(conn, myUUID)
		}

	default:
		log.Fatal("Invalid protocol. Use 'tcp', 'udp', or 'auto'")
	}
}

func establishTCPConnection(signalServer, myUUID, targetUUID string) (net.Conn, error) {
	options := client.StunTurnOptions{
		SignalServer:         signalServer,
		Key:                  myUUID,
		IP:                   targetUUID,
		TryCount:             300,
		TimeoutSeconds:       10,
		StunDiscoveryTimeout: 10 * time.Second,
	}

	stunTurn := client.New(options)
	
	fmt.Println("Getting TCP peer...")
	peerResp, err := stunTurn.GetTCPPeer()
	if err != nil {
		return nil, err
	}
	
	fmt.Printf("Peer info: %s (local port: %d)\n", peerResp.PeerAddress, peerResp.LocalPort)
	fmt.Println("Attempting TCP hole punch...")
	
	// Set the peer response and punch hole
	stunTurn.SetPeerResponse(peerResp)
	return stunTurn.PunchTCPHole()
}

func establishUDPConnection(signalServer, myUUID, targetUUID string) (*net.UDPConn, error) {
	options := client.StunTurnOptions{
		SignalServer:         signalServer,
		Key:                  myUUID,
		IP:                   targetUUID,
		TryCount:             300,
		TimeoutSeconds:       10,
		StunDiscoveryTimeout: 10 * time.Second,
	}

	stunTurn := client.New(options)
	
	fmt.Println("Getting UDP peer...")
	peerResp, err := stunTurn.GetUDPPeer()
	if err != nil {
		return nil, err
	}
	
	fmt.Printf("Peer info: %s (local port: %d)\n", peerResp.PeerAddress, peerResp.LocalPort)
	fmt.Println("Attempting UDP hole punch...")
	
	// Set the peer response and punch hole
	stunTurn.SetPeerResponse(peerResp)
	return stunTurn.PunchUDPHole()
}

func establishAutoConnection(signalServer, myUUID, targetUUID string) (net.Conn, *net.UDPConn, string, error) {
	options := client.StunTurnOptions{
		SignalServer:         signalServer,
		Key:                  myUUID,
		IP:                   targetUUID,
		TryCount:             300,
		TimeoutSeconds:       10,
		StunDiscoveryTimeout: 10 * time.Second,
	}

	stunTurn := client.New(options)
	
	fmt.Println("Getting client peer (auto-detect)...")
	peerResp, err := stunTurn.GetClientPeer()
	if err != nil {
		return nil, nil, "", err
	}
	
	fmt.Printf("Peer info: %s (protocol: %s, local port: %d)\n", 
		peerResp.PeerAddress, peerResp.Protocol, peerResp.LocalPort)
	
	// Set the peer response
	stunTurn.SetPeerResponse(peerResp)
	
	if peerResp.Protocol == "udp" {
		fmt.Println("Attempting UDP hole punch...")
		udpConn, err := stunTurn.PunchUDPHole()
		return nil, udpConn, "udp", err
	} else {
		fmt.Println("Attempting TCP hole punch...")
		conn, err := stunTurn.PunchTCPHole()
		return conn, nil, "tcp", err
	}
}

// Helper function to update peer response in StunTurn (needed for hole punching)
func updatePeerResponse(st *client.StunTurn, resp *client.PeerResponse) {
	// This is a workaround since peerResponse is not exported
	// In a real implementation, you might want to modify the client library
	// to have a SetPeerResponse method or make the field exported
	// For now, we'll call the hole punch methods directly with the response
}

func startTCPChat(conn net.Conn, myUUID string) {
	defer conn.Close()
	
	fmt.Printf("\n=== P2P TCP Chat Started (You are: %s) ===\n", myUUID)
	fmt.Println("Type messages and press Enter. Type 'quit' to exit.")
	fmt.Println("========================================")

	// Start goroutine to read messages from peer
	go func() {
		buffer := make([]byte, 1024)
		for {
			n, err := conn.Read(buffer)
			if err != nil {
				fmt.Println("\nPeer disconnected.")
				os.Exit(0)
			}
			message := strings.TrimSpace(string(buffer[:n]))
			if message != "" {
				fmt.Printf("Peer: %s\n", message)
			}
		}
	}()

	// Read input from user and send to peer
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}
		
		message := scanner.Text()
		if strings.ToLower(message) == "quit" {
			break
		}
		
		if message != "" {
			_, err := conn.Write([]byte(message))
			if err != nil {
				fmt.Println("Failed to send message:", err)
				break
			}
		}
	}
}

func startUDPChat(conn *net.UDPConn, myUUID string) {
	defer conn.Close()
	
	fmt.Printf("\n=== P2P UDP Chat Started (You are: %s) ===\n", myUUID)
	fmt.Println("Type messages and press Enter. Type 'quit' to exit.")
	fmt.Println("========================================")

	// We need to determine the peer address from the last received packet
	var peerAddr *net.UDPAddr

	// Start goroutine to read messages from peer
	go func() {
		buffer := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				fmt.Println("\nError reading UDP message:", err)
				continue
			}
			
			// Update peer address
			if peerAddr == nil {
				peerAddr = addr
			}
			
			message := strings.TrimSpace(string(buffer[:n]))
			if message != "" && message != "ping" {
				fmt.Printf("Peer: %s\n", message)
			}
		}
	}()

	// Wait a moment for the first ping/pong to establish peer address
	time.Sleep(1 * time.Second)

	// Read input from user and send to peer
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}
		
		message := scanner.Text()
		if strings.ToLower(message) == "quit" {
			break
		}
		
		if message != "" && peerAddr != nil {
			_, err := conn.WriteToUDP([]byte(message), peerAddr)
			if err != nil {
				fmt.Println("Failed to send message:", err)
				break
			}
		} else if peerAddr == nil {
			fmt.Println("Waiting for peer connection...")
		}
	}
}
