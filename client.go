package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/stun/v2"
)

// STUNServers is a list of public STUN servers
var STUNServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	// Add more if needed
}

var (
	clientID       = flag.String("id", "", "Unique ID for this client (required)")
	connectToID    = flag.String("connect", "", "ID of the peer to connect to (optional, initiator only)")
	signalServer   = flag.String("signal", "ws://localhost:8080/ws", "Address of the signaling server")
	udpPort        = flag.Int("udp", 0, "Local UDP port to use (0=random)")
	localUDPAddr   *net.UDPAddr
	publicUDPAddr  *net.UDPAddr // Our public address discovered via STUN
	peerPublicAddr *net.UDPAddr // The peer's public address received via signaling
	udpConn        *net.UDPConn
	connMutex      sync.Mutex // To protect access to udpConn and peerPublicAddr
	wsConn         *websocket.Conn
)

// DiscoverPublicAddress uses STUN to find the public UDP address
func DiscoverPublicAddress(laddr *net.UDPAddr) (*net.UDPAddr, error) {
	log.Println("Attempting STUN discovery...")
	var discoveredAddr *net.UDPAddr
	var lastErr error

	// Use the local UDP connection for STUN queries
	stunConn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on UDP for STUN: %w", err)
	}
	defer stunConn.Close()

	log.Printf("STUN client listening on: %s", stunConn.LocalAddr())

	for _, server := range STUNServers {
		log.Printf("Querying STUN server: %s", server)
		addr, err := GetMappedAddress(stunConn, server)
		if err == nil {
			log.Printf("STUN Success! Public address: %s", addr)
			discoveredAddr = addr
			break // Success!
		}
		log.Printf("STUN query failed for %s: %v", server, err)
		lastErr = err
	}

	if discoveredAddr == nil {
		return nil, fmt.Errorf("failed to discover public address using STUN after trying all servers: %w", lastErr)
	}
	return discoveredAddr, nil
}

// GetMappedAddress performs a single STUN request
func GetMappedAddress(conn stun.Connection, serverAddr string) (*net.UDPAddr, error) {
	client, err := stun.NewClient(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create STUN client: %w", err)
	}
	defer client.Close()

	message, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to build STUN request: %w", err)
	}

	var mappedAddr stun.XORMappedAddress
	var callbackErr error
	var wg sync.WaitGroup
	wg.Add(1)

	// Define callback for handling the STUN response
	callback := func(res stun.Event) {
		defer wg.Done()
		if res.Message.Type != stun.BindingSuccess {
			callbackErr = fmt.Errorf("stun request failed, type: %s", res.Message)
			return
		}
		if err := mappedAddr.GetFrom(res.Message); err != nil {
			callbackErr = fmt.Errorf("failed to get XOR mapped address from response: %w", err)
			return
		}
		log.Printf("Received STUN response: mapped address %s", mappedAddr.String())
	}

	// Send the STUN request
	err = client.Do(message, callback)
	if err != nil {
		return nil, fmt.Errorf("failed to send STUN request to %s: %w", serverAddr, err)
	}

	// Wait for the callback to complete (with a timeout)
	waitChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		// Callback completed
		if callbackErr != nil {
			return nil, callbackErr
		}
		udpAddr := &net.UDPAddr{IP: mappedAddr.IP, Port: mappedAddr.Port}
		return udpAddr, nil
	case <-time.After(5 * time.Second): // Timeout for STUN response
		return nil, fmt.Errorf("stun request to %s timed out", serverAddr)
	}
}

// StartUDPServer binds to a local UDP port
func StartUDPServer() error {
	var err error
	localUDPAddr = &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: *udpPort}

	// ListenPacket is necessary for UDP
	tempConn, err := net.ListenUDP("udp", localUDPAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", *udpPort, err)
	}
	localUDPAddr = tempConn.LocalAddr().(*net.UDPAddr) // Get actual port if 0 was specified
	udpConn = tempConn                                 // Assign to global variable
	log.Printf("UDP listener started on: %s", localUDPAddr.String())
	return nil
}

// ListenForUDPMessages reads incoming UDP packets
func ListenForUDPMessages(ctx context.Context) {
	if udpConn == nil {
		log.Println("UDP connection is not initialized")
		return
	}
	log.Printf("Listening for UDP packets on %s", localUDPAddr.String())
	buffer := make([]byte, 1500) // MTU size buffer

	for {
		select {
		case <-ctx.Done():
			log.Println("UDP listener stopped.")
			return
		default:
			// Set a read deadline to prevent blocking indefinitely
			udpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, remoteAddr, err := udpConn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Expected timeout, continue listening
				}
				log.Printf("Error reading from UDP: %v", err)
				continue // Continue loop on other errors for now
			}

			message := string(buffer[:n])
			log.Printf("UDP Received from %s: %s", remoteAddr.String(), message)

			connMutex.Lock()
			if peerPublicAddr != nil && peerPublicAddr.String() == remoteAddr.String() {
				log.Printf("<<< Direct P2P message received from peer! >>>")
				// Handle the direct message here
			} else if peerPublicAddr == nil {
				log.Printf("Received UDP from unknown source %s, peer address not yet known.", remoteAddr)
			} else {
				log.Printf("Received UDP from %s, but expected peer address is %s.", remoteAddr, peerPublicAddr)
			}
			connMutex.Unlock()

		}
	}
}

// HandleSignaling manages WebSocket communication
func HandleSignaling(ctx context.Context) {
	conn, _, err := websocket.DefaultDialer.Dial(*signalServer, nil)
	if err != nil {
		log.Fatalf("Failed to connect to signaling server %s: %v", *signalServer, err)
	}
	wsConn = conn
	defer wsConn.Close()

	log.Println("Connected to signaling server")

	// 1. Register with Signaling Server
	regMsg := Message{Type: "register", Payload: *clientID}
	if err := wsConn.WriteJSON(regMsg); err != nil {
		log.Fatalf("Failed to send registration message: %v", err)
	}

	// Start a goroutine to read messages from the signaling server
	go func() {
		for {
			var msg Message
			err := wsConn.ReadJSON(&msg)
			if err != nil {
				// Check if context was cancelled, which might close the connection
				select {
				case <-ctx.Done():
					log.Println("WebSocket read loop ending due to context cancellation.")
				default:
					log.Printf("Error reading from WebSocket: %v. Closing connection.", err)
				}
				return // Exit goroutine on error or context cancellation
			}

			log.Printf("Signal Received: %+v", msg)

			switch msg.Type {
			case "info":
				log.Printf("INFO from Server: %s", msg.Message)
				// After successful registration, potentially initiate offer
				if strings.HasPrefix(msg.Message, "Registered successfully") && *connectToID != "" {
					go initiateConnection()
				}
			case "error":
				log.Printf("ERROR from Server: %s", msg.Message)
				if msg.Message == "Client ID already in use" {
					log.Fatalf("Exiting because Client ID is already in use.") // Exit if ID is taken
				}
			case "offer":
				// Received an offer from a peer
				if peerAddrStr, ok := msg.Payload.(string); ok {
					addr, err := net.ResolveUDPAddr("udp", peerAddrStr)
					if err != nil {
						log.Printf("Error parsing peer address from offer: %v", err)
						continue
					}
					log.Printf("Received connection OFFER from %s with address %s", msg.SenderID, addr.String())
					connMutex.Lock()
					peerPublicAddr = addr
					connMutex.Unlock()

					// Send back an answer
					if publicUDPAddr != nil {
						answerMsg := Message{
							Type:     "answer",
							TargetID: msg.SenderID, // Send back to the offer sender
							Payload:  publicUDPAddr.String(),
						}
						log.Printf("Sending ANSWER to %s with my public address %s", msg.SenderID, publicUDPAddr.String())
						if err := wsConn.WriteJSON(answerMsg); err != nil {
							log.Printf("Error sending answer: %v", err)
						}
						// Start hole punching now that we have the peer's address
						go startHolePunching()
					} else {
						log.Println("Cannot send answer: My public address is not known yet.")
					}
				} else {
					log.Printf("Invalid payload type for offer: expected string, got %T", msg.Payload)
				}
			case "answer":
				// Received an answer from the peer we sent an offer to
				if peerAddrStr, ok := msg.Payload.(string); ok {
					addr, err := net.ResolveUDPAddr("udp", peerAddrStr)
					if err != nil {
						log.Printf("Error parsing peer address from answer: %v", err)
						continue
					}
					log.Printf("Received connection ANSWER from %s with address %s", msg.SenderID, addr.String())

					connMutex.Lock()
					if peerPublicAddr != nil && peerPublicAddr.String() == addr.String() {
						log.Println("Received duplicate answer or already have peer address, ignoring.")
						connMutex.Unlock()
						continue
					}
					peerPublicAddr = addr
					connMutex.Unlock()

					// We initiated the connection, now we have the peer's addr, start hole punching
					go startHolePunching()

				} else {
					log.Printf("Invalid payload type for answer: expected string, got %T", msg.Payload)
				}
			default:
				log.Printf("Unknown message type from signaling server: %s", msg.Type)
			}
		}
	}()

	// Keep the main signaling handler alive until context is cancelled
	<-ctx.Done()
	log.Println("Signaling handler stopping.")
	// Attempt graceful WebSocket closure
	wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	time.Sleep(time.Second) // Give time for close message to send
	wsConn.Close()
}

// initiateConnection starts the connection process if -connect flag is set
func initiateConnection() {
	if *connectToID == "" {
		return // Not initiating a connection
	}

	connMutex.Lock()
	myAddr := publicUDPAddr
	connMutex.Unlock()

	if myAddr == nil {
		log.Println("Cannot initiate connection: My public address is not discovered yet.")
		// Maybe add a retry mechanism or wait signal
		return
	}

	log.Printf("Initiating connection to peer %s", *connectToID)
	offerMsg := Message{
		Type:     "offer",
		TargetID: *connectToID,
		Payload:  myAddr.String(),
	}

	log.Printf("Sending OFFER to %s with my public address %s", *connectToID, myAddr.String())
	connMutex.Lock()
	defer connMutex.Unlock()
	if wsConn == nil {
		log.Println("Cannot send offer: WebSocket connection is not available.")
		return
	}
	if err := wsConn.WriteJSON(offerMsg); err != nil {
		log.Printf("Error sending offer: %v", err)
	}
}

// startHolePunching sends initial UDP packets to try opening the NAT pinhole
func startHolePunching() {
	connMutex.Lock()
	peerAddr := peerPublicAddr
	localConn := udpConn
	connMutex.Unlock()

	if peerAddr == nil || localConn == nil {
		log.Println("Cannot start hole punching: Peer address or local UDP connection is missing.")
		return
	}

	log.Printf("Starting UDP hole punching towards %s...", peerAddr.String())

	// Send a few packets quickly to increase chances
	for i := 0; i < 5; i++ {
		message := fmt.Sprintf("Punch-%s-%d", *clientID, i)
		_, err := localConn.WriteToUDP([]byte(message), peerAddr)
		if err != nil {
			log.Printf("Error sending punch packet %d to %s: %v", i, peerAddr, err)
		} else {
			//log.Printf("Sent punch packet %d to %s", i, peerAddr) // Verbose log
		}
		time.Sleep(100 * time.Millisecond) // Small delay between punches
	}
	log.Println("Initial hole punching packets sent.")
	// UDP listener (ListenForUDPMessages) will detect incoming packets
	// Now just periodically send a keep-alive or heartbeat
	go sendKeepAlive(peerAddr)
}

// sendKeepAlive periodically sends packets to keep the NAT mapping open
func sendKeepAlive(peerAddr *net.UDPAddr) {
	if udpConn == nil || peerAddr == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second) // Send keep-alive every 15 seconds
	defer ticker.Stop()

	log.Printf("Starting keep-alive sender to %s", peerAddr.String())

	for range ticker.C {
		connMutex.Lock()
		localConn := udpConn
		currentPeerAddr := peerPublicAddr // Re-check in case it changed (though unlikely here)
		connMutex.Unlock()

		if localConn == nil || currentPeerAddr == nil || currentPeerAddr.String() != peerAddr.String() {
			log.Println("Stopping keep-alive sender: connection state changed.")
			return
		}

		keepAliveMsg := "keep-alive-" + *clientID
		_, err := localConn.WriteToUDP([]byte(keepAliveMsg), peerAddr)
		if err != nil {
			log.Printf("Error sending keep-alive to %s: %v", peerAddr.String(), err)
			// Consider stopping if errors persist?
		} else {
			//log.Printf("Sent keep-alive to %s", peerAddr.String()) // Verbose
		}
	}
}

// SendDirectUDPMessage allows sending messages via the console after connection
func SendDirectUDPMessage(peerAddr *net.UDPAddr, message string) {
	connMutex.Lock()
	localConn := udpConn
	currentPeerAddr := peerPublicAddr
	connMutex.Unlock()

	if localConn == nil || currentPeerAddr == nil {
		log.Println("Cannot send message: UDP connection or peer address is not set.")
		return
	}

	if peerAddr.String() != currentPeerAddr.String() {
		log.Println("Warning: Sending message to an address different from the registered peer.")
		// Allow sending anyway for testing, but normally you'd only send to currentPeerAddr
	}

	log.Printf("Sending UDP message to %s: %s", peerAddr.String(), message)
	_, err := localConn.WriteToUDP([]byte(message), peerAddr)
	if err != nil {
		log.Printf("Error sending direct UDP message to %s: %v", peerAddr.String(), err)
	}
}

func main() {
	flag.Parse()

	if *clientID == "" {
		// Generate a unique ID if none provided
		// *clientID = uuid.New().String()[:8] // Less predictable than simple counter
		log.Fatal("Error: -id flag is required.") // Make ID mandatory
	}
	log.Printf("Client ID: %s", *clientID)
	log.Printf("Signaling Server: %s", *signalServer)
	if *connectToID != "" {
		log.Printf("Attempting to connect to Peer ID: %s", *connectToID)
	} else {
		log.Printf("Waiting for connections...")
	}

	// 1. Start local UDP listener
	err := StartUDPServer()
	if err != nil {
		log.Fatalf("Fatal error starting UDP server: %v", err)
	}
	log.Printf("UDP Server listening on: %s", localUDPAddr.String())

	// 2. Discover Public Address using STUN (using the same UDP port)
	// Run discovery in a goroutine so we can connect to signal server simultaneously
	var wgStun sync.WaitGroup
	wgStun.Add(1)
	go func() {
		defer wgStun.Done()
		pubAddr, err := DiscoverPublicAddress(localUDPAddr) // Pass the specific local addr
		if err != nil {
			log.Printf("WARN: Failed to discover public address via STUN: %v. Hole punching might fail.", err)
			// Proceed anyway, maybe it works on LAN or with UPnP/PCP enabled routers implicitly
		} else {
			connMutex.Lock()
			publicUDPAddr = pubAddr
			connMutex.Unlock()
			log.Printf("Successfully discovered public UDP address: %s", publicUDPAddr.String())
		}
	}()

	// Context for managing goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cleanup on exit

	// 3. Connect to Signaling Server and handle messages
	var wgSignal sync.WaitGroup
	wgSignal.Add(1)
	go func() {
		defer wgSignal.Done()
		HandleSignaling(ctx)
	}()

	// 4. Start listening for UDP messages from peers
	var wgUDPListen sync.WaitGroup
	wgUDPListen.Add(1)
	go func() {
		defer wgUDPListen.Done()
		ListenForUDPMessages(ctx)
		log.Println("UDP Listening goroutine finished.") // Debug log
	}()

	// Wait for STUN discovery to complete before potentially initiating
	wgStun.Wait()
	log.Println("STUN discovery attempt finished.")

	// Small delay to allow registration before initiating connection
	// Better approach: wait for registration confirmation message ("info") in HandleSignaling
	// time.Sleep(2 * time.Second) // Removed, initiateConnection moved to signal handler

	// 5. Start console reader for sending messages
	log.Println("Enter message to send to peer (or 'quit' to exit):")
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		text, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("Error reading input: %v", err)
			break // Exit on input error
		}
		text = strings.TrimSpace(text)

		if strings.ToLower(text) == "quit" {
			break
		}

		connMutex.Lock()
		peerAddr := peerPublicAddr // Get current peer address under lock
		connMutex.Unlock()

		if peerAddr != nil {
			SendDirectUDPMessage(peerAddr, text)
		} else {
			log.Println("Cannot send message: Peer address not yet known.")
		}
	}

	// Signal goroutines to stop and wait for them
	log.Println("Exiting application...")
	cancel() // Signal context cancellation

	log.Println("Waiting for Signaling handler to stop...")
	wgSignal.Wait() // Wait for signal handler goroutine to finish
	log.Println("Signaling handler stopped.")

	// Close the UDP connection here, which should unblock the listener
	if udpConn != nil {
		log.Println("Closing UDP connection...")
		udpConn.Close()
	}

	log.Println("Waiting for UDP listener to stop...")
	wgUDPListen.Wait() // Wait for UDP listener to finish
	log.Println("UDP listener stopped.")

	log.Println("Application finished.")
}
