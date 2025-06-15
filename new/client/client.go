// file: p2p_client_roles.go
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

func main() {
	// --- Argument Parsing ---
	if len(os.Args) < 4 {
		printUsage()
		return
	}
	serverAddr := os.Args[1]
	role := os.Args[2]
	uuid := os.Args[3]
	var targetIP string

	if role == "initiator" {
		if len(os.Args) < 5 {
			log.Println("Initiator role requires a target IP.")
			printUsage()
			return
		}
		targetIP = os.Args[4]
	} else if role != "receiver" {
		log.Printf("Invalid role: %s", role)
		printUsage()
		return
	}

	// --- Connect and Signal ---
	log.Printf("Starting client in '%s' role.", role)
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	localPort := conn.LocalAddr().(*net.TCPAddr).Port
	log.Printf("✅ Connected. We are using local port: %d. Sending hello...", localPort)

	hello := ClientHello{Role: role, UUID: uuid, TargetIP: targetIP}
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		log.Fatalf("Failed to send hello to server: %v", err)
	}

	log.Println("Waiting for server to find peer...")
	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Fatalf("Failed to receive response from server: %v", err)
	}

	if resp.Error != "" {
		log.Fatalf("❌ Server returned an error: %s", resp.Error)
	}

	peerAddr := resp.PeerAddress
	log.Printf("✅ Received peer address: %s", peerAddr)
	conn.Close()

	// --- Hole Punch and Chat (Unchanged) ---
	log.Println("Starting TCP hole punching...")
	p2pConn, err := punchHole(localPort, peerAddr)
	if err != nil {
		log.Fatalf("❌ Hole punching failed: %v", err)
	}
	log.Println("✅ P2P connection established!")
	chat(p2pConn)
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  go run . <server_addr> receiver <uuid>")
	fmt.Println("  go run . <server_addr> initiator <uuid> <target_ip>")
}

// punchHole and chat functions are IDENTICAL to the cross-platform client version.
// They are included here for completeness. setReuseAddr is in the other files.

func punchHole(localPort int, remoteAddrStr string) (net.Conn, error) {
	remoteAddr, err := net.ResolveTCPAddr("tcp", remoteAddrStr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve remote address: %w", err)
	}

	localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: localPort}
	connChan := make(chan net.Conn)
	errChan := make(chan error, 2)

	controlFunc := func(network, address string, c syscall.RawConn) error {
		var controlErr error
		err := c.Control(func(fd uintptr) { controlErr = setReuseAddr(fd) })
		if err != nil {
			return err
		}
		return controlErr
	}

	dialer := &net.Dialer{LocalAddr: localAddr, Timeout: 5 * time.Second, Control: controlFunc}
	go func() {
		for range 200 {
			if conn, err := dialer.Dial("tcp", remoteAddr.String()); err == nil {
				connChan <- conn
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	lc := net.ListenConfig{Control: controlFunc}
	go func() {
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
		select {
		case conn := <-connChan:
			return conn, nil
		case <-time.After(20 * time.Second):
			return nil, fmt.Errorf("both attempts failed, first error: %w", err)
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
