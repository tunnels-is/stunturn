package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Client represents a connected peer
type Client struct {
	ID   string
	Conn *websocket.Conn
}

// Message structure for signaling
type Message struct {
	Type     string `json:"type"`               // "register", "offer", "answer", "error", "info"
	TargetID string `json:"targetId,omitempty"` // ID of the peer to send the message to
	Payload  any    `json:"payload,omitempty"`  // Could be STUN info (IP:Port string) or other data
	SenderID string `json:"senderId,omitempty"` // ID of the peer sending the message
	Message  string `json:"message,omitempty"`  // For error or info messages
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			// Allow all origins for simplicity in this example
			return true
		},
	}
	// Thread-safe map to store connected clients
	clients = make(map[string]*Client)
	mutex   = &sync.Mutex{}
)

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error upgrading connection: %v", err)
		return
	}
	defer ws.Close()

	var currentClient *Client

	log.Println("Client connected from:", ws.RemoteAddr())

	for {
		// Read message from WebSocket client
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("Error reading message: %v", err)
			// Remove client on disconnect/error
			if currentClient != nil {
				removeClient(currentClient.ID)
			}
			break
		}

		log.Printf("Received message: %+v", msg)

		switch msg.Type {
		case "register":
			clientID, ok := msg.Payload.(string)
			if !ok || clientID == "" {
				log.Println("Invalid register payload")
				sendError(ws, "Invalid client ID for registration")
				continue
			}
			mutex.Lock()
			if _, exists := clients[clientID]; exists {
				mutex.Unlock()
				log.Printf("Client ID %s already registered", clientID)
				sendError(ws, "Client ID already in use")
				// Optionally disconnect old client or deny new one
				// ws.Close() // For now, let the new one fail registration
				continue
			}
			currentClient = &Client{ID: clientID, Conn: ws}
			clients[clientID] = currentClient
			mutex.Unlock()
			log.Printf("Client registered: %s", clientID)
			sendInfo(ws, "Registered successfully with ID: "+clientID)

		case "offer", "answer":
			if currentClient == nil {
				sendError(ws, "Must register first")
				continue
			}
			if msg.TargetID == "" {
				sendError(ws, "Target ID is required for offer/answer")
				continue
			}

			mutex.Lock()
			targetClient, exists := clients[msg.TargetID]
			mutex.Unlock()

			if !exists {
				log.Printf("Target client %s not found for message type %s from %s", msg.TargetID, msg.Type, currentClient.ID)
				sendError(ws, "Target client not found: "+msg.TargetID)
				continue
			}

			// Add sender ID to the message before relaying
			msg.SenderID = currentClient.ID

			// Relay message to target client
			log.Printf("Relaying %s from %s to %s", msg.Type, currentClient.ID, msg.TargetID)
			err = targetClient.Conn.WriteJSON(msg)
			if err != nil {
				log.Printf("Error relaying message to %s: %v", msg.TargetID, err)
				// Inform sender maybe?
				sendError(ws, "Failed to send message to target "+msg.TargetID)
				// Consider removing target client if connection is broken
			}

		default:
			log.Printf("Unknown message type received: %s", msg.Type)
			if currentClient != nil {
				sendError(ws, "Unknown message type: "+msg.Type)
			}
		}
	}

	// Cleanup if loop exits unexpectedly before registration
	if currentClient != nil {
		removeClient(currentClient.ID)
	}
	log.Println("Client disconnected:", ws.RemoteAddr())
}

func removeClient(clientID string) {
	mutex.Lock()
	defer mutex.Unlock()
	client, exists := clients[clientID]
	if exists {
		log.Printf("Removing client: %s", clientID)
		client.Conn.Close() // Close WebSocket connection
		delete(clients, clientID)
	}
}

func sendError(conn *websocket.Conn, errMsg string) {
	msg := Message{Type: "error", Message: errMsg}
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("Error sending error message: %v", err)
	}
}

func sendInfo(conn *websocket.Conn, infoMsg string) {
	msg := Message{Type: "info", Message: infoMsg}
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("Error sending info message: %v", err)
	}
}

func main1() {
	// Configure endpoint
	http.HandleFunc("/ws", handleConnections)

	// Start server
	port := "8080" // Make sure this port is accessible publicly
	log.Println("Signaling server starting on port", port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
