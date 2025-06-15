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

type ClientHello struct {
	UUID     string `json:"uuid"`
	TargetIP string `json:"target_ip"`
}

type ServerResponse struct {
	PeerAddress string `json:"peer_address"`
	Error       string `json:"error,omitempty"`
}

func main() {
	serverAddr := os.Args[1]
	role := os.Args[2]
	uuid := os.Args[3]
	var targetIP string

	if role == "client" {
		targetIP = os.Args[4]
	}

	err, localPort, peerAddress := getRemovePeeringAddress(serverAddr, uuid, targetIP)
	if err != nil {
		panic(err)
	}

	p2pConn, err := punchHole(localPort, peerAddress)
	if err != nil {
		log.Fatalf("❌ Hole punching failed: %v", err)
	}
	log.Println("✅ P2P connection established!")
	chat(p2pConn)
}

func getRemovePeeringAddress(signalServer, key, ip string) (err error, localPort int, peerAddress string) {
	conn, err := net.Dial("tcp", signalServer)
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return err, 0, ""
	}

	hello := ClientHello{UUID: key, TargetIP: ip}
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return err, 0, ""
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err, 0, ""
	}

	return nil, conn.LocalAddr().(*net.TCPAddr).Port, resp.PeerAddress
}

func punchHole(localPort int, remoteAddrStr string) (net.Conn, error) {
	remoteAddr, err := net.ResolveTCPAddr("tcp", remoteAddrStr)
	if err != nil {
		return nil, err
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

	killGoroutines := make(chan byte, 10)
	dialer := &net.Dialer{LocalAddr: localAddr, Timeout: 2 * time.Second, Control: controlFunc}
	go func() {
		defer func() {
			fmt.Println("dialer exiting")
		}()
		for range 300 {
			select {
			case <-killGoroutines:
				return
			default:
			}
			if conn, err := dialer.Dial("tcp", remoteAddr.String()); err == nil {
				connChan <- conn
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	lc := net.ListenConfig{Control: controlFunc}
	go func() {
		defer func() {
			fmt.Println("listener exiting")
		}()
		var err error
		for range 300 {
			select {
			case <-killGoroutines:
				return
			default:
			}
			var listener net.Listener
			time.Sleep(100 * time.Millisecond)
			listener, err = lc.Listen(context.Background(), "tcp", localAddr.String())
			if err != nil {
				continue
			}
			defer listener.Close()
			// listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
			var conn net.Conn
			if conn, err = listener.Accept(); err == nil {
				connChan <- conn
				break
			} else {
				continue
			}
		}
		errChan <- err
	}()

	defer func() {
		killGoroutines <- 1
		killGoroutines <- 1
	}()

	select {
	case conn := <-connChan:
		return conn, nil
	case err = <-errChan:
		select {
		case conn := <-connChan:
			return conn, err
		case <-time.After(12 * time.Second):
			return nil, fmt.Errorf("both attempts failed, first error")
		}
	case <-time.After(10 * time.Second):
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
			// log.Printf("Connection lost: %v", err)
			return
		}
	}
}
