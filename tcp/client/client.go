// file: p2p_client_crossplatform.go
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

	log.Println("Connecting to server to get peer address...")
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	localPort := conn.LocalAddr().(*net.TCPAddr).Port
	log.Printf("✅ Connected. We are using local port: %d. Waiting for peer...", localPort)

	var peerSignal Signal
	if err := json.NewDecoder(conn).Decode(&peerSignal); err != nil {
		log.Fatalf("Failed to receive peer address from server: %v", err)
	}
	peerAddr := peerSignal.Address
	// log.Printf("✅ Received peer address: %s", peerAddr)

	conn.Close()

	log.Println("Starting TCP hole punching...")
	p2pConn, err := punchHole(localPort, peerAddr)
	if err != nil {
		log.Fatalf("❌ Hole punching failed: %v", err)
	}
	log.Println("✅ P2P connection established!")

	chat(p2pConn)
}

// punchHole now uses a cross-platform helper function 'setReuseAddr'.
func punchHole(localPort int, remoteAddrStr string) (net.Conn, error) {
	remoteAddr, err := net.ResolveTCPAddr("tcp", remoteAddrStr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve remote address: %w", err)
	}
	localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: localPort}
	connChan := make(chan net.Conn)
	errChan := make(chan error, 2)

	// Define the control function that uses our cross-platform helper.
	// This function will be used for both the dialer and the listener.
	controlFunc := func(network, address string, c syscall.RawConn) error {
		var controlErr error
		err := c.Control(func(fd uintptr) {
			// Call our platform-specific helper function.
			controlErr = setReuseAddr(fd)
		})
		if err != nil {
			return err
		}
		return controlErr
	}

	dialer := &net.Dialer{
		LocalAddr: localAddr,
		Timeout:   5 * time.Second,
		Control:   controlFunc,
	}

	go func() {
		// log.Printf("Attempting to dial %s from local port %d", remoteAddr, localPort)
		for range 100 {
			if conn, err := dialer.Dial("tcp", remoteAddr.String()); err == nil {
				connChan <- conn
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	lc := net.ListenConfig{
		Control: controlFunc,
	}

	go func() {
		// log.Printf("Attempting to listen on local port %d", localPort)
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
		case <-time.After(30 * time.Second):
			return nil, fmt.Errorf("both dialing and listening failed, first error: %w", err)
		}
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("hole punching timed out")
	}
}

// chat function remains unchanged.
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
