// file: p2p_client_final.go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// Signal represents the data sent between client and server.
type Signal struct {
	Address string `json:"address"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <server_addr (e.g., localhost:8080)>", os.Args[0])
	}
	serverAddr := os.Args[1]

	// --- 1. Connect to server for Signaling and STUN ---
	log.Println("Connecting to server to get peer address...")
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	// Defer closing this connection until main() returns.
	defer conn.Close()

	// The local port of this connection is what we need for hole punching.
	localPort := conn.LocalAddr().(*net.TCPAddr).Port
	log.Printf("✅ Connected. We are using local port: %d. Waiting for peer...", localPort)

	// --- 2. Wait for the server to send us the peer's address ---
	var peerSignal Signal
	if err := json.NewDecoder(conn).Decode(&peerSignal); err != nil {
		log.Fatalf("Failed to receive peer address from server: %v", err)
	}
	peerAddr := peerSignal.Address
	log.Printf("✅ Received peer address: %s", peerAddr)

	// We've received the signal, so we can now close the connection to the server.
	// This frees up the connection but keeps the NAT mapping alive for a short time.
	conn.Close()

	// --- 3. Perform TCP Hole Punching with the critical SO_REUSEADDR fix ---
	log.Println("Starting TCP hole punching...")
	p2pConn, err := punchHole(localPort, peerAddr)
	if err != nil {
		log.Fatalf("❌ Hole punching failed: %v", err)
	}
	log.Println("✅ P2P connection established!")

	// --- 4. Communicate! ---
	chat(p2pConn)
}

// punchHole now uses a net.ListenConfig with a Control function to set SO_REUSEADDR.
func punchHole(localPort int, remoteAddrStr string) (net.Conn, error) {
	remoteAddr, err := net.ResolveTCPAddr("tcp", remoteAddrStr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve remote address: %w", err)
	}
	localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: localPort}
	connChan := make(chan net.Conn)
	errChan := make(chan error, 2)

	// The dialer also needs to use the same local port.
	dialer := &net.Dialer{
		LocalAddr: localAddr,
		Timeout:   5 * time.Second,
		// The Control function is used to set socket options before the connection is established.
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			})
		},
	}

	go func() {
		log.Printf("Attempting to dial %s from local port %d", remoteAddr, localPort)
		for i := 0; i < 100; i++ {
			if conn, err := dialer.Dial("tcp", remoteAddr.String()); err == nil {
				connChan <- conn
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()

	// The listener needs the SO_REUSEADDR option to bind to the port in TIME_WAIT state.
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			})
		},
	}

	go func() {
		log.Printf("Attempting to listen on local port %d", localPort)
		listener, err := lc.Listen(context.Background(), "tcp", localAddr.String())
		if err != nil {
			errChan <- err
			return
		}
		defer listener.Close()

		listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
		if conn, err := listener.Accept(); err == nil {
			connChan <- conn
		} else {
			errChan <- err
		}
	}()

	select {
	case conn := <-connChan:
		return conn, nil
	case err := <-errChan:
		log.Printf("One attempt failed: %v. Waiting for the other...", err)
		select {
		case conn := <-connChan:
			return conn, nil
		case <-time.After(20 * time.Second):
			return nil, fmt.Errorf("both dialing and listening failed, first error: %w", err)
		}
	case <-time.After(20 * time.Second):
		return nil, fmt.Errorf("hole punching timed out")
	}
}

func chat(conn net.Conn) {
	defer conn.Close()
	log.Println("Enter a message and press Enter. Use 'exit' to quit.")
	go func() {
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			fmt.Printf("\n[PEER]: %s\n> ", scanner.Text())
		}
		log.Println("Peer has disconnected.")
		os.Exit(0)
	}()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		if strings.ToLower(text) == "exit" {
			return
		}
		if _, err := conn.Write([]byte(text + "\n")); err != nil {
			log.Printf("Connection lost: %v", err)
			return
		}
	}
}
