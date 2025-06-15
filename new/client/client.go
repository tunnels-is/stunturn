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
	Protocol string `json:"protocol"`
}

type ServerResponse struct {
	Protocol    string `json:"protocol"`
	PeerAddress string `json:"peer_address"`
	Error       string `json:"error,omitempty"`
}

func main() {
	serverAddr := os.Args[1]
	uuid := os.Args[2]
	proto := os.Args[3]

	var targetIP string
	if len(os.Args) > 4 {
		targetIP = os.Args[4]
	}

	err, localPort, peerAddress, protocol := getRemovePeeringAddress(serverAddr, uuid, targetIP, proto)
	if err != nil {
		panic(err)
	}

	p2pConn, err := punchHole(localPort, peerAddress, protocol)
	if err != nil {
		log.Fatalf("❌ Hole punching failed: %v", err)
	}
	log.Println("✅ P2P connection established!")
	chat(p2pConn)
}

func getRemovePeeringAddress(signalServer, key, ip, proto string) (err error, localPort int, peerAddress string, protocol string) {
	conn, err := net.Dial("tcp", signalServer)
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return err, 0, "", ""
	}

	hello := ClientHello{UUID: key, TargetIP: ip}
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return err, 0, "", ""
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err, 0, "", ""
	}

	return nil, conn.LocalAddr().(*net.TCPAddr).Port, resp.PeerAddress, resp.Protocol
}

func punchHole(localPort int, remoteAddrStr string, protocol string) (net.Conn, error) {
	var dialer *net.Dialer
	var lc net.ListenConfig
	var rAddr string
	var lAddr string
	if protocol == "TCP" {
		remoteAddr, err := net.ResolveTCPAddr(protocol, remoteAddrStr)
		if err != nil {
			return nil, err
		}
		rAddr = remoteAddr.String()

		localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: localPort}
		lAddr = localAddr.String()

		controlFunc := func(network, address string, c syscall.RawConn) error {
			var controlErr error
			err := c.Control(func(fd uintptr) { controlErr = setReuseAddr(fd) })
			if err != nil {
				return err
			}
			return controlErr
		}
		dialer = &net.Dialer{LocalAddr: localAddr, Timeout: 2 * time.Second, Control: controlFunc}
		lc = net.ListenConfig{Control: controlFunc}

	} else {
		remoteAddr, err := net.ResolveUDPAddr(protocol, remoteAddrStr)
		if err != nil {
			return nil, err
		}
		rAddr = remoteAddr.String()

		localAddr := &net.UDPAddr{IP: net.IPv4zero, Port: localPort}
		lAddr = localAddr.String()
		dialer = &net.Dialer{LocalAddr: localAddr, Timeout: 2 * time.Second}
		lc = net.ListenConfig{}
	}

	connChan := make(chan net.Conn)
	errChan := make(chan error, 2)
	killGoroutines := make(chan byte, 10)
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
			if conn, err := dialer.Dial(protocol, rAddr); err == nil {
				connChan <- conn
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

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
			listener, err = lc.Listen(context.Background(), protocol, lAddr)
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
	case err := <-errChan:
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
