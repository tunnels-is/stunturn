package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// StunTurn represents a STUN/TURN client with configuration
type StunTurn struct {
	SignalServer        string
	Dialer              *net.Dialer
	TryCount            int
	TimeoutSeconds      int
	UDPDiscoveryTimeout time.Duration
	Key                 string
	IP                  string
	PeerResponse        *PeerResponse
	TLSConfig           *tls.Config
	IsTLSServer         bool
}

// StunTurnOptions contains configuration options for StunTurn
type StunTurnOptions struct {
	SignalServer        string
	Dialer              *net.Dialer
	TryCount            int           // Number of hole punching attempts (default: 300)
	TimeoutSeconds      int           // Timeout for hole punching in seconds (default: 10)
	UDPDiscoveryTimeout time.Duration // Timeout for UDP Address discovery (default: 10 seconds)
	Key                 string        // UUID key for peer identification
	IP                  string        // Target IP address
	TLSConfig           *tls.Config   // TLS configuration (optional, for encrypted connections)
	IsTLSServer         bool          // Whether this peer should act as TLS server
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

	stunDiscoveryTimeout := opts.UDPDiscoveryTimeout
	if stunDiscoveryTimeout == 0 {
		stunDiscoveryTimeout = 10 * time.Second // Default STUN discovery timeout
	}

	// Setup TLS configuration
	var tlsConfig *tls.Config
	if opts.TLSConfig != nil {
		// Use the provided TLS configuration
		tlsConfig = opts.TLSConfig
	}

	return &StunTurn{
		SignalServer:        opts.SignalServer,
		Dialer:              opts.Dialer,
		TryCount:            tryCount,
		TimeoutSeconds:      timeoutSeconds,
		UDPDiscoveryTimeout: stunDiscoveryTimeout,
		Key:                 opts.Key,
		IP:                  opts.IP,
		TLSConfig:           tlsConfig,
		IsTLSServer:         opts.IsTLSServer,
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
func (st *StunTurn) GetTCPPeer() (err error) {
	var conn net.Conn
	if st.Dialer == nil {
		conn, err = net.Dial("tcp4", st.SignalServer)
	} else {
		conn, err = st.Dialer.Dial("tcp4", st.SignalServer)
	}
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return err
	}

	hello := ClientHello{UUID: st.Key, TargetIP: st.IP, Protocol: "tcp"}
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return err
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}

	st.PeerResponse = &PeerResponse{
		Protocol:    resp.Protocol,
		LocalPort:   conn.LocalAddr().(*net.TCPAddr).Port,
		PeerAddress: resp.PeerAddress,
	}
	return nil
}

// GetClientPeer establishes a client peer connection (auto-detects TCP/UDP)
func (st *StunTurn) GetClientPeer() (err error) {
	var conn net.Conn
	if st.Dialer != nil {
		conn, err = st.Dialer.Dial("tcp4", st.SignalServer)
	} else {
		conn, err = net.Dial("tcp4", st.SignalServer)
	}
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return err
	}

	udpcon, udpaddr, err := st.discoverUdpAddr()
	if err != nil {
		return err
	}

	hello := ClientHello{UUID: st.Key, Protocol: "", UDPAddress: udpaddr.String()}
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return err
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if resp.Protocol == "tcp" {
		st.PeerResponse = &PeerResponse{
			Protocol:    resp.Protocol,
			LocalPort:   conn.LocalAddr().(*net.TCPAddr).Port,
			PeerAddress: resp.PeerAddress,
		}
		return nil
	}

	st.PeerResponse = &PeerResponse{
		Protocol:    resp.Protocol,
		LocalPort:   udpaddr.Port,
		PeerAddress: resp.PeerAddress,
		UDPAddr:     udpaddr,
		UDPConn:     udpcon,
	}
	return nil
}

// GetUDPPeer establishes a UDP peer connection through the signal server
func (st *StunTurn) GetUDPPeer() (err error) {
	var conn net.Conn
	if st.Dialer != nil {
		conn, err = st.Dialer.Dial("tcp4", st.SignalServer)
	} else {
		conn, err = net.Dial("tcp4", st.SignalServer)
	}
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return err
	}

	udpcon, udpaddr, err := st.discoverUdpAddr()
	if err != nil {
		return err
	}

	hello := ClientHello{UUID: st.Key, TargetIP: st.IP, Protocol: "udp", UDPAddress: udpaddr.String()}
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return err
	}

	var resp ServerResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}

	st.PeerResponse = &PeerResponse{
		Protocol:    resp.Protocol,
		LocalPort:   udpaddr.Port,
		PeerAddress: resp.PeerAddress,
		UDPAddr:     udpaddr,
		UDPConn:     udpcon,
	}
	return nil
}

// PunchUDPHole attempts to establish a UDP hole punch connection
func (st *StunTurn) PunchUDPHole() (uc *net.UDPConn, err error) {
	if st.PeerResponse == nil {
		return nil, fmt.Errorf("missing PeerResponse")
	}
	peerAddress, err := net.ResolveUDPAddr("udp4", st.PeerResponse.PeerAddress)

	killGoroutines := make(chan byte, 10)
	defer func() {
		killGoroutines <- 1
	}()

	go func() {
		for range st.TryCount {
			select {
			case <-killGoroutines:
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
			if _, err := st.PeerResponse.UDPConn.WriteToUDP([]byte("ping"), peerAddress); err != nil {
				continue
			}
		}
	}()

	buf := make([]byte, 1024)
	start := time.Now()
	for range st.TryCount {
		n, _, err := st.PeerResponse.UDPConn.ReadFromUDP(buf)
		if err != nil {
			if time.Since(start).Seconds() > float64(st.TimeoutSeconds) {
				return nil, fmt.Errorf("20 second UDP read timeout: %s", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if string(buf[:n]) == "ping" {
			return st.PeerResponse.UDPConn, nil
		}
	}
	return nil, errors.New("Unable to punch UDP hole")
}

// PunchTCPHole attempts to establish a TCP hole punch connection
func (st *StunTurn) PunchTCPHole() (net.Conn, error) {
	if st.PeerResponse == nil {
		return nil, fmt.Errorf("missing PeerResponse")
	}
	var rAddr string
	remoteAddr, err := net.ResolveTCPAddr("tcp4", st.PeerResponse.PeerAddress)
	if err != nil {
		return nil, err
	}
	rAddr = remoteAddr.String()

	localAddr := &net.TCPAddr{IP: net.IPv4zero, Port: st.PeerResponse.LocalPort}
	dialer := st.Dialer
	if dialer == nil {
		dialer = &net.Dialer{LocalAddr: localAddr, Timeout: 5 * time.Second}
	}

	dialer.Control = controlFunc
	dialer.LocalAddr = localAddr
	connChan := make(chan net.Conn)
	errChan := make(chan error, 2)
	killGoroutines := make(chan byte, 10)
	go func() {
		for range st.TryCount {
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
		for range st.TryCount {
			select {
			case <-killGoroutines:
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
			var listener net.Listener
			listener, err = st.getTCPListener(st.PeerResponse.LocalPort)
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
		case <-time.After(time.Duration(st.TimeoutSeconds) * time.Second):
			return nil, fmt.Errorf("hole punching timed out, err: %s", outErr)
		}
	}
}

// PunchTCPHoleTLS attempts to establish a TLS-enabled TCP hole punch connection
func (st *StunTurn) PunchTCPHoleTLS() (net.Conn, error) {
	if st.TLSConfig == nil {
		return nil, errors.New("TLS configuration not provided")
	}

	// First establish the regular TCP connection
	tcpConn, err := st.PunchTCPHole()
	if err != nil {
		return nil, err
	}

	// Wrap the connection with TLS
	if st.IsTLSServer {
		// This peer acts as TLS server
		tlsConn := tls.Server(tcpConn, st.TLSConfig)
		err = tlsConn.Handshake()
		if err != nil {
			tcpConn.Close()
			return nil, fmt.Errorf("TLS server handshake failed: %v", err)
		}
		return tlsConn, nil
	} else {
		// This peer acts as TLS client
		tlsConn := tls.Client(tcpConn, st.TLSConfig)
		err = tlsConn.Handshake()
		if err != nil {
			tcpConn.Close()
			return nil, fmt.Errorf("TLS client handshake failed: %v", err)
		}
		return tlsConn, nil
	}
}

// Convenience methods using default configuration

func (st *StunTurn) discoverUdpAddr() (*net.UDPConn, *net.UDPAddr, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, nil, err
	}

	stunAddr, err := net.ResolveUDPAddr("udp", st.SignalServer)
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.WriteToUDP([]byte("ping"), stunAddr); err != nil {
		return nil, nil, err
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(st.UDPDiscoveryTimeout))
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
