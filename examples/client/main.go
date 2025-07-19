package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/tunnels-is/stunturn/client"
)

func main() {
	ip := flag.String("ip", "", "target ip")
	key := flag.String("key", "", "shared key")
	proto := flag.String("proto", "tcp", "protocol (tcp/udp)")
	flag.Parse()
	ipaddr := ""
	if ip != nil {
		ipaddr = *ip
	}
	st := client.New(client.StunTurnOptions{
		SignalServer:        "192.248.170.119:1111",
		Dialer:              nil,
		TryCount:            100,
		TimeoutSeconds:      30 * time.Second,
		UDPDiscoveryTimeout: 5 * time.Second,
		Key:                 *key,
		IP:                  ipaddr,
		TLSConfig:           nil,
		IsTLSServer:         false,
	})

	if *ip == "" {
		err := st.GetClientPeer()
		if err != nil {
			panic(err)
		}
		if st.PeerResponse.Protocol == "tcp" {
			tcpCon, err := st.PunchTCPHole()
			if err != nil {
				panic(err)
			}
			startTCPChat(tcpCon)
		} else {
			udpCon, err := st.PunchUDPHole()
			if err != nil {
				panic(err)
			}
			startUDPChat(udpCon)
		}
	} else {
		if *proto == "tcp" {
			err := st.GetTCPPeer()
			if err != nil {
				panic(err)
			}
			tcpCon, err := st.PunchTCPHole()
			if err != nil {
				panic(err)
			}
			startTCPChat(tcpCon)
		} else {
			err := st.GetUDPPeer()
			if err != nil {
				panic(err)
			}
			udpCon, err := st.PunchUDPHole()
			if err != nil {
				panic(err)
			}
			startUDPChat(udpCon)
		}
	}
}

func startTCPChat(conn net.Conn) {
	fmt.Println("TCP CHAT STARTED")
	defer conn.Close()

	// Start goroutine to read messages from peer
	go func() {
		buffer := make([]byte, 1024)
		for {
			n, err := conn.Read(buffer)
			if err != nil {
				fmt.Println("\nPeer disconnected.")
				os.Exit(0)
			}
			message := strings.TrimSpace(string(buffer[:n]))
			if message != "" {
				fmt.Printf("Peer: %s\n", message)
			}
		}
	}()

	// Read input from user and send to peer
	scanner := bufio.NewScanner(os.Stdin)
	for {
		if !scanner.Scan() {
			break
		}

		message := scanner.Text()
		if strings.ToLower(message) == "quit" {
			break
		}

		if message != "" {
			fmt.Println("You:", string(message))
			_, err := conn.Write([]byte(message))
			if err != nil {
				fmt.Println("Failed to send message:", err)
				break
			}
		}
	}
}

func startUDPChat(conn *net.UDPConn) {
	defer conn.Close()

	// We need to determine the peer address from the last received packet
	var peerAddr *net.UDPAddr

	// Start goroutine to read messages from peer
	go func() {
		buffer := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				fmt.Println("\nError reading UDP message:", err)
				continue
			}

			// Update peer address
			if peerAddr == nil {
				peerAddr = addr
			}

			message := strings.TrimSpace(string(buffer[:n]))
			if message != "" && message != "ping" {
				fmt.Printf("Peer: %s\n", message)
			}
		}
	}()

	// Wait a moment for the first ping/pong to establish peer address
	time.Sleep(1 * time.Second)

	// Read input from user and send to peer
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}

		message := scanner.Text()
		if strings.ToLower(message) == "quit" {
			break
		}

		if message != "" && peerAddr != nil {
			_, err := conn.WriteToUDP([]byte(message), peerAddr)
			if err != nil {
				fmt.Println("Failed to send message:", err)
				break
			}
		} else if peerAddr == nil {
			fmt.Println("Waiting for peer connection...")
		}
	}
}
