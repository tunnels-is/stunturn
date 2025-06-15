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

func main() {
	serverAddr := os.Args[1]
	uuid := os.Args[2]

	var targetIP string
	var proto string
	isServer := false
	if len(os.Args) == 4 {
		isServer = true
	} else if len(os.Args) == 5 {
		proto = os.Args[3]
		targetIP = os.Args[4]
	}

	if isServer {
		resp, err := getClientPeer(serverAddr, uuid, targetIP)
		if err != nil {
			panic(err)
		}
		fmt.Println(err, resp)
		if resp.Protocol == "udp" {
			p2pConn, err := punchUDPHole(resp)
			if err != nil {
				log.Fatalf("❌ Hole punching failed: %v", err)
			}
			log.Println("✅ P2P UDP connection established!")
			chat(p2pConn)

		} else {
			p2pConn, err := puncTCPhHole(resp)
			if err != nil {
				log.Fatalf("❌ Hole punching failed: %v", err)
			}
			log.Println("✅ P2P TCP connection established!")
			chat(p2pConn)
		}
		return
	}

	if proto == "tcp" {
		resp, err := getTCPPeer(serverAddr, uuid, targetIP)
		if err != nil {
			panic(err)
		}
		fmt.Println(err, resp)

		p2pConn, err := puncTCPhHole(resp)
		if err != nil {
			log.Fatalf("❌ Hole punching failed: %v", err)
		}
		log.Println("✅ P2P TCP connection established!")
		chat(p2pConn)

	} else if proto == "udp" {
		resp, err := getUDPPeer(serverAddr, uuid, targetIP)
		if err != nil {
			panic(err)
		}
		fmt.Println(err, resp)
		p2pConn, err := punchUDPHole(resp)
		if err != nil {
			log.Fatalf("❌ Hole punching failed: %v", err)
		}
		log.Println("✅ P2P UDP connection established!")
		chat(p2pConn)
	}
}

func getTCPPeer(signalServer, key, ip string) (tcpresp *PeerResponse, err error) {
	conn, err := net.Dial("tcp4", signalServer)
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return nil, err
	}

	hello := ClientHello{UUID: key, TargetIP: ip, Protocol: "tcp"}
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return nil, err
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}

	return &PeerResponse{
		Protocol:    resp.Protocol,
		localPort:   conn.LocalAddr().(*net.TCPAddr).Port,
		peerAddress: resp.PeerAddress,
	}, nil
}

type PeerResponse struct {
	Protocol    string
	localPort   int
	peerAddress string
	UDPAddr     *net.UDPAddr
	UDPConn     *net.UDPConn
}

func getClientPeer(signalServer, key, ip string) (udpresp *PeerResponse, err error) {
	conn, err := net.Dial("tcp4", signalServer)
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return nil, err
	}

	udpcon, udpaddr, err := discoverUdpAddr(signalServer)
	if err != nil {
		return nil, err
	}

	hello := ClientHello{UUID: key, TargetIP: ip, Protocol: "", UDPAddress: udpaddr.String()}
	fmt.Println("SENDING:", hello)
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return nil, err
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	if resp.Protocol == "tcp" {
		return &PeerResponse{
			Protocol:    resp.Protocol,
			localPort:   conn.LocalAddr().(*net.TCPAddr).Port,
			peerAddress: resp.PeerAddress,
		}, nil
	}

	return &PeerResponse{
		Protocol:    resp.Protocol,
		localPort:   udpaddr.Port,
		peerAddress: resp.PeerAddress,
		UDPAddr:     udpaddr,
		UDPConn:     udpcon,
	}, nil

}

func getUDPPeer(signalServer, key, ip string) (udpresp *PeerResponse, err error) {
	conn, err := net.Dial("tcp4", signalServer)
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return nil, err
	}

	udpcon, udpaddr, err := discoverUdpAddr(signalServer)
	if err != nil {
		return nil, err
	}

	hello := ClientHello{UUID: key, TargetIP: ip, Protocol: "udp", UDPAddress: udpaddr.String()}
	fmt.Println("SENDING:", hello)
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return nil, err
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}

	return &PeerResponse{
		Protocol:    resp.Protocol,
		localPort:   udpaddr.Port,
		peerAddress: resp.PeerAddress,
		UDPAddr:     udpaddr,
		UDPConn:     udpcon,
	}, nil
}

func punchUDPHole(resp *PeerResponse) (err error) {
	peerAddress, err := net.ResolveUDPAddr("udp4", resp.peerAddress)

	killGoroutines := make(chan byte, 10)
	defer func() {
		killGoroutines <- 1
	}()

	go func() {
		for range 300 {
			select {
			case <-killGoroutines:
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
			if _, err := resp.UDPConn.WriteToUDP([]byte("ping"), peerAddress); err != nil {
				continue
			}
		}
	}()

	buf := make([]byte, 1024)
	start := time.Now()
	for {
		n, _, err := resp.UDPConn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println(err)
			if time.Since(start).Seconds() > 20 {
				return fmt.Errorf("20 second UDP read timeout: %s", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		fmt.Println(string(buf[:n]), buf[:n])
		if string(buf[:n]) == "ping" {
			return nil
		}
	}
}

func puncTCPhHole(resp *PeerResponse) (net.Conn, error) {
	var rAddr string
	remoteAddr, err := net.ResolveTCPAddr("tcp4", resp.peerAddress)
	if err != nil {
		return nil, err
	}
	rAddr = remoteAddr.String()

	localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: resp.localPort}
	dialer := &net.Dialer{LocalAddr: localAddr, Timeout: 2 * time.Second, Control: controlFunc}

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
				time.Sleep(100 * time.Millisecond)
			}
			if conn, err := dialer.Dial("tcp", rAddr); err == nil {
				connChan <- conn
				return
			}
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
				time.Sleep(100 * time.Millisecond)
			}
			var listener net.Listener
			listener, err = getTCPListener(resp.localPort)
			if err != nil {
				continue
			}
			defer listener.Close()
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
	buff := make([]byte, 1024)
	go func() {
		for {
			n, err := conn.Read(buff[0:])
			if err != nil {
				fmt.Println("read err", err)
				return
			}
			fmt.Println(string(buff[:n]))
		}
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
func chatUDP(resp *PeerResponse) {
	defer resp.UDPConn.Close()
	log.Println("Enter a message and press Enter. Use 'exit' to quit.")
	buff := make([]byte, 1024)
	go func() {
		for {
			n, err := resp.UDPConn.Read(buff[0:])
			if err != nil {
				fmt.Println("read err", err)
				return
			}
			fmt.Println(string(buff[:n]))
		}
	}()

	pa, err := net.ResolveUDPAddr("udp", resp.peerAddress)
	if err != nil {
		fmt.Println(err)
		return
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		if strings.ToLower(text) == "exit" {
			return
		}
		_, err := resp.UDPConn.WriteToUDP([]byte(text+"\n"), pa)
		if err != nil {
			log.Printf("Connection lost: %v", err)

			return
		}
	}
}

func discoverUdpAddr(stunServerAddr string) (*net.UDPConn, *net.UDPAddr, error) {
	// We listen on a random port. This is our local UDP socket.
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, nil, err
	}

	// Send a packet to the STUN server to open the NAT port.
	stunAddr, err := net.ResolveUDPAddr("udp", stunServerAddr)
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.WriteToUDP([]byte("ping"), stunAddr); err != nil {
		return nil, nil, err
	}

	// Wait for the response from the STUN server.
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, nil, err
	}

	// The response is our public address.
	publicAddr, err := net.ResolveUDPAddr("udp", string(buf[:n]))
	if err != nil {
		return nil, nil, err
	}

	return conn, publicAddr, nil
}

func getTCPListener(localPort int) (l net.Listener, err error) {
	localAddr := &net.UDPAddr{IP: net.IPv4zero, Port: localPort}
	lc := net.ListenConfig{Control: controlFunc}
	l, err = lc.Listen(context.Background(), "tcp", localAddr.String())
	return
}

var controlFunc = func(network, address string, c syscall.RawConn) error {
	var controlErr error
	err := c.Control(func(fd uintptr) { controlErr = setReuseAddr(fd) })
	if err != nil {
		return err
	}
	return controlErr
}
