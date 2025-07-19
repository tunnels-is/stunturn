package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// StunTurn represents a STUN/TURN client with configuration
type StunTurn struct {
	signalServer         string
	dialer               *net.Dialer
	tryCount             int
	timeoutSeconds       int
	stunDiscoveryTimeout time.Duration
	key                  string
	ip                   string
	peerResponse         *PeerResponse
}

// StunTurnOptions contains configuration options for StunTurn
type StunTurnOptions struct {
	SignalServer         string
	Dialer               *net.Dialer
	TryCount             int           // Number of hole punching attempts (default: 300)
	TimeoutSeconds       int           // Timeout for hole punching in seconds (default: 10)
	StunDiscoveryTimeout time.Duration // Timeout for STUN discovery (default: 10 seconds)
	Key                  string        // UUID key for peer identification
	IP                   string        // Target IP address
}

// New creates a new StunTurn instance with the provided options
func New(opts StunTurnOptions) *StunTurn {
	// Set default values
	tryCount := opts.TryCount
	if tryCount == 0 {
		tryCount = 300 // Default try count
	}

	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 10 // Default timeout
	}

	stunDiscoveryTimeout := opts.StunDiscoveryTimeout
	if stunDiscoveryTimeout == 0 {
		stunDiscoveryTimeout = 10 * time.Second // Default STUN discovery timeout
	}

	return &StunTurn{
		signalServer:         opts.SignalServer,
		dialer:               opts.Dialer,
		tryCount:             tryCount,
		timeoutSeconds:       timeoutSeconds,
		stunDiscoveryTimeout: stunDiscoveryTimeout,
		key:                  opts.Key,
		ip:                   opts.IP,
	}
}

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

type PeerResponse struct {
	Protocol    string
	LocalPort   int
	PeerAddress string
	UDPAddr     *net.UDPAddr
	UDPConn     *net.UDPConn
}

// GetTCPPeer establishes a TCP peer connection through the signal server
func (st *StunTurn) GetTCPPeer() (tcpresp *PeerResponse, err error) {
	var conn net.Conn
	if st.dialer == nil {
		conn, err = net.Dial("tcp4", st.signalServer)
	} else {
		conn, err = st.dialer.Dial("tcp4", st.signalServer)
	}
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return nil, err
	}

	hello := ClientHello{UUID: st.key, TargetIP: st.ip, Protocol: "tcp"}
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

// GetClientPeer establishes a client peer connection (auto-detects TCP/UDP)
func (st *StunTurn) GetClientPeer() (res *PeerResponse, err error) {
	var conn net.Conn
	if st.dialer != nil {
		conn, err = st.dialer.Dial("tcp4", st.signalServer)
	} else {
		conn, err = net.Dial("tcp4", st.signalServer)
	}
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return nil, err
	}

	udpcon, udpaddr, err := st.discoverUdpAddr()
	if err != nil {
		return nil, err
	}

	hello := ClientHello{UUID: st.key, TargetIP: st.ip, Protocol: "", UDPAddress: udpaddr.String()}
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

// GetUDPPeer establishes a UDP peer connection through the signal server
func (st *StunTurn) GetUDPPeer() (udpresp *PeerResponse, err error) {
	var conn net.Conn
	if st.dialer != nil {
		conn, err = st.dialer.Dial("tcp4", st.signalServer)
	} else {
		conn, err = net.Dial("tcp4", st.signalServer)
	}
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return nil, err
	}

	udpcon, udpaddr, err := st.discoverUdpAddr()
	if err != nil {
		return nil, err
	}

	hello := ClientHello{UUID: st.key, TargetIP: st.ip, Protocol: "udp", UDPAddress: udpaddr.String()}
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

// PunchUDPHole attempts to establish a UDP hole punch connection
func (st *StunTurn) PunchUDPHole() (uc *net.UDPConn, err error) {
	peerAddress, err := net.ResolveUDPAddr("udp4", st.peerResponse.PeerAddress)

	killGoroutines := make(chan byte, 10)
	defer func() {
		killGoroutines <- 1
	}()

	go func() {
		for range st.tryCount {
			select {
			case <-killGoroutines:
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
			if _, err := st.peerResponse.UDPConn.WriteToUDP([]byte("ping"), peerAddress); err != nil {
				continue
			}
		}
	}()

	buf := make([]byte, 1024)
	start := time.Now()
	for range st.tryCount {
		n, _, err := st.peerResponse.UDPConn.ReadFromUDP(buf)
		if err != nil {
			if time.Since(start).Seconds() > float64(st.timeoutSeconds) {
				return nil, fmt.Errorf("20 second UDP read timeout: %s", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if string(buf[:n]) == "ping" {
			return st.peerResponse.UDPConn, nil
		}
	}
	return nil, errors.New("Unable to punch UDP hole")
}

// PunchTCPHole attempts to establish a TCP hole punch connection
func (st *StunTurn) PunchTCPHole() (net.Conn, error) {
	var rAddr string
	remoteAddr, err := net.ResolveTCPAddr("tcp4", st.peerResponse.PeerAddress)
	if err != nil {
		return nil, err
	}
	rAddr = remoteAddr.String()

	localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: st.peerResponse.LocalPort}
	dialer := st.dialer
	if dialer == nil {
		dialer = &net.Dialer{LocalAddr: localAddr, Timeout: 5 * time.Second}
	}

	dialer.Control = controlFunc
	dialer.LocalAddr = localAddr
	connChan := make(chan net.Conn)
	errChan := make(chan error, 2)
	killGoroutines := make(chan byte, 10)
	go func() {
		for range st.tryCount {
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
			select {
			case errChan <- err:
			default:
			}
		}
	}()

	go func() {
		var err error
		for range st.tryCount {
			select {
			case <-killGoroutines:
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
			var listener net.Listener
			listener, err = st.getTCPListener(st.peerResponse.LocalPort)
			if err != nil {
				select {
				case errChan <- err:
				default:
				}
				continue
			}
			defer listener.Close()
			var conn net.Conn
			if conn, err = listener.Accept(); err == nil {
				connChan <- conn
				return
			}
			select {
			case errChan <- err:
			default:
			}
			continue

		}
	}()

	defer func() {
		killGoroutines <- 1
		killGoroutines <- 1
	}()

	var outErr error
	for {
		select {
		case conn := <-connChan:
			return conn, nil
		case outErr = <-errChan:
		case <-time.After(time.Duration(st.timeoutSeconds) * time.Second):
			return nil, fmt.Errorf("hole punching timed out, err: %s", outErr)
		}
	}
}

// Convenience methods using default configuration

func (st *StunTurn) discoverUdpAddr() (*net.UDPConn, *net.UDPAddr, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, nil, err
	}

	stunAddr, err := net.ResolveUDPAddr("udp", st.signalServer)
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.WriteToUDP([]byte("ping"), stunAddr); err != nil {
		return nil, nil, err
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(st.stunDiscoveryTimeout))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, nil, err
	}
	conn.SetReadDeadline(time.Time{})

	publicAddr, err := net.ResolveUDPAddr("udp", string(buf[:n]))
	if err != nil {
		return nil, nil, err
	}

	return conn, publicAddr, nil
}

func (st *StunTurn) getTCPListener(localPort int) (l net.Listener, err error) {
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
