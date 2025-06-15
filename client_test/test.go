package client

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/zveinn/stunturn/client"
)

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
		resp, err := client.GetClientPeer(serverAddr, uuid, targetIP)
		if err != nil {
			panic(err)
		}
		fmt.Println(err, resp)
		if resp.Protocol == "udp" {
			err := client.PunchUDPHole(resp)
			if err != nil {
				log.Fatalf("❌ Hole punching failed: %v", err)
			}
			log.Println("✅ P2P UDP connection established!")
			ChatUDP(resp)

		} else {
			p2pConn, err := client.PuncTCPhHole(resp)
			if err != nil {
				log.Fatalf("❌ Hole punching failed: %v", err)
			}
			log.Println("✅ P2P TCP connection established!")
			Chat(p2pConn)
		}
		return
	}

	if proto == "tcp" {
		resp, err := client.GetTCPPeer(serverAddr, uuid, targetIP)
		if err != nil {
			panic(err)
		}
		fmt.Println(err, resp)

		p2pConn, err := client.PuncTCPhHole(resp)
		if err != nil {
			log.Fatalf("❌ Hole punching failed: %v", err)
		}
		log.Println("✅ P2P TCP connection established!")
		Chat(p2pConn)

	} else if proto == "udp" {
		resp, err := client.GetUDPPeer(serverAddr, uuid, targetIP)
		if err != nil {
			panic(err)
		}
		fmt.Println(err, resp)
		err = client.PunchUDPHole(resp)
		if err != nil {
			log.Fatalf("❌ Hole punching failed: %v", err)
		}
		log.Println("✅ P2P UDP connection established!")
		ChatUDP(resp)
	}
}
func Chat(conn net.Conn) {
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

func ChatUDP(resp *client.PeerResponse) {
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

	pa, err := net.ResolveUDPAddr("udp", resp.PeerAddress)
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
