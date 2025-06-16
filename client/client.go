package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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

func GetTCPPeer(signalServer, key, ip string) (tcpresp *PeerResponse, err error) {
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
		LocalPort:   conn.LocalAddr().(*net.TCPAddr).Port,
		PeerAddress: resp.PeerAddress,
	}, nil
}

type PeerResponse struct {
	Protocol    string
	LocalPort   int
	PeerAddress string
	UDPAddr     *net.UDPAddr
	UDPConn     *net.UDPConn
}

func GetClientPeer(signalServer, key, ip string) (udpresp *PeerResponse, err error) {
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
			LocalPort:   conn.LocalAddr().(*net.TCPAddr).Port,
			PeerAddress: resp.PeerAddress,
		}, nil
	}

	return &PeerResponse{
		Protocol:    resp.Protocol,
		LocalPort:   udpaddr.Port,
		PeerAddress: resp.PeerAddress,
		UDPAddr:     udpaddr,
		UDPConn:     udpcon,
	}, nil

}

func GetUDPPeer(signalServer, key, ip string) (udpresp *PeerResponse, err error) {
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
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return nil, err
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}

	return &PeerResponse{
		Protocol:    resp.Protocol,
		LocalPort:   udpaddr.Port,
		PeerAddress: resp.PeerAddress,
		UDPAddr:     udpaddr,
		UDPConn:     udpcon,
	}, nil
}

func PunchUDPHole(resp *PeerResponse) (err error) {
	peerAddress, err := net.ResolveUDPAddr("udp4", resp.PeerAddress)

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
			if time.Since(start).Seconds() > 20 {
				return fmt.Errorf("20 second UDP read timeout: %s", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if string(buf[:n]) == "ping" {
			return nil
		}
	}
}

func PuncTCPhHole(resp *PeerResponse) (net.Conn, error) {
	var rAddr string
	remoteAddr, err := net.ResolveTCPAddr("tcp4", resp.PeerAddress)
	if err != nil {
		return nil, err
	}
	rAddr = remoteAddr.String()

	localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: resp.LocalPort}
	dialer := &net.Dialer{LocalAddr: localAddr, Timeout: 2 * time.Second, Control: controlFunc}

	connChan := make(chan net.Conn)
	errChan := make(chan error, 2)
	killGoroutines := make(chan byte, 10)
	go func() {
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
		var err error
		for range 300 {
			select {
			case <-killGoroutines:
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
			var listener net.Listener
			listener, err = getTCPListener(resp.LocalPort)
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

func discoverUdpAddr(stunServerAddr string) (*net.UDPConn, *net.UDPAddr, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, nil, err
	}

	stunAddr, err := net.ResolveUDPAddr("udp", stunServerAddr)
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.WriteToUDP([]byte("ping"), stunAddr); err != nil {
		return nil, nil, err
	}

	buf := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, nil, err
	}

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
