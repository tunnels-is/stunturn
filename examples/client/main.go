package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
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
	tl := flag.String("tls", "", "set tls server or client (server/client)")
	genCert := flag.String("gen-cert", "", "generate certificate for given IP address")
	flag.Parse()

	// If gen-cert flag is provided, generate certificate and exit
	if genCert != nil && *genCert != "" {
		err := generateCertificateForIP(*genCert)
		if err != nil {
			fmt.Printf("Error generating certificate: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ipaddr := ""
	if ip != nil {
		ipaddr = *ip
	}
	var tc *tls.Config
	if tl != nil {
		if *tl == "client" {
			pub, err := os.ReadFile("./cert.pem")
			if err != nil {
				panic(err)
			}
			rootPool := x509.NewCertPool()
			if !rootPool.AppendCertsFromPEM(pub) {
				fmt.Println("Client: failed to append cert to pool")
				return
			}

			tc = &tls.Config{
				MinVersion:       tls.VersionTLS13,
				MaxVersion:       tls.VersionTLS13,
				CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.CurveP521},
				ServerName:       ipaddr,
				RootCAs:          rootPool,
			}

		} else {
			pub, _ := os.ReadFile("./cert.pem")
			pb, _ := os.ReadFile("./key.pem")
			tlscert, err := tls.X509KeyPair(pub, pb)
			if err != nil {
				panic(err)
			}
			tc = &tls.Config{
				MinVersion:       tls.VersionTLS13,
				MaxVersion:       tls.VersionTLS13,
				CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.CurveP521},
				Certificates:     []tls.Certificate{tlscert},
			}
		}
	}

	isServer := false
	if tl != nil {
		if *tl == "client" {
			isServer = false
		} else {
			isServer = true
		}
	}

	st := client.New(client.StunTurnOptions{
		SignalServer:        "192.248.170.119:1111",
		Dialer:              nil,
		TryCount:            100,
		TimeoutSeconds:      30 * time.Second,
		UDPDiscoveryTimeout: 5 * time.Second,
		Key:                 *key,
		IP:                  ipaddr,
		TLSConfig:           tc,
		IsTLSServer:         isServer,
	})

	if *ip == "" {
		err := st.GetClientPeer()
		if err != nil {
			panic(err)
		}
		if st.PeerResponse.Protocol == "tcp" {
			if st.TLSConfig != nil {
				tcpCon, err := st.PunchTCPHoleTLS()
				if err != nil {
					panic(err)
				}
				startTCPChat(tcpCon)
			} else {
				tcpCon, err := st.PunchTCPHole()
				if err != nil {
					panic(err)
				}
				startTCPChat(tcpCon)
			}
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
			if st.TLSConfig != nil {
				tcpCon, err := st.PunchTCPHoleTLS()
				if err != nil {
					panic(err)
				}
				startTCPChat(tcpCon)
			} else {
				tcpCon, err := st.PunchTCPHole()
				if err != nil {
					panic(err)
				}
				startTCPChat(tcpCon)
			}
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

// generateCertificateForIP generates a self-signed certificate for the given IP address
// and saves it to cert.pem and key.pem files
func generateCertificateForIP(ipStr string) error {
	// Parse the IP address
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", ipStr)
	}

	// Generate RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Test Organization"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"Test City"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{ip},
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %v", err)
	}

	// Save certificate to cert.pem
	certOut, err := os.Create("cert.pem")
	if err != nil {
		return fmt.Errorf("failed to create cert.pem: %v", err)
	}
	defer certOut.Close()

	err = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err != nil {
		return fmt.Errorf("failed to write certificate: %v", err)
	}

	// Save private key to key.pem
	keyOut, err := os.Create("key.pem")
	if err != nil {
		return fmt.Errorf("failed to create key.pem: %v", err)
	}
	defer keyOut.Close()

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %v", err)
	}

	err = pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	if err != nil {
		return fmt.Errorf("failed to write private key: %v", err)
	}

	fmt.Printf("Generated certificate for IP %s (cert.pem and key.pem)\n", ipStr)
	return nil
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
