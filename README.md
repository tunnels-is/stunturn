# STUNTURN - NAT Hole Punching Library
A Go library for establishing peer-to-peer connections through NAT/firewalls using hole punching techniques. This library pairs `initiators` and `receivers` using a shared `access_key` (UUID) to facilitate direct P2P connections.

## Features
- **TCP and UDP hole punching** - Support for both protocols
- **Cross-platform** - Works on Linux, macOS (Darwin), and Windows  
- **Configurable timeouts and retry counts** - Customizable connection parameters
- **Built-in STUN server** - Automatic public IP/port discovery for UDP
- **Concurrent connection attempts** - Multiple simultaneous connection strategies
- **Socket reuse** - Efficient port reuse for better NAT traversal

## Public Signaling Server
We will be launching a public signaling server @ signal.tunnels.is this week (free of use).

## Architecture Overview
This implementation uses the concepts of `initiator` and `receiver` to enable efficient scaling:

- **Initiator**: Opens a long-lived connection to the signaling server and waits for a matching receiver (30-second timeout)
- **Receiver**: Makes a single HTTP request to check if any initiators are waiting (returns immediately if none found)

This design prevents socket buildup on both client and server sides during the signaling process.


## Installation

```bash
go get github.com/tunnels-is/stunturn
```

## Quick Start

### Basic TCP Connection
```go
package main

import (
    "log"
    "github.com/tunnels-is/stunturn/client"
)

func main() {
    // Initiator side
    resp, err := client.GetTCPPeer(nil, "signalserver:port", "shared-uuid", "target-ip")
    if err != nil {
        log.Fatal(err)
    }
    
    conn, err := client.PuncTCPhHole(resp, nil, 300, 10)
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()
    
    // Use the P2P connection
    conn.Write([]byte("Hello P2P!"))
}
```

### Basic UDP Connection
```go
package main

import (
    "log"
    "github.com/tunnels-is/stunturn/client"
)

func main() {
    // Initiator side
    resp, err := client.GetUDPPeer(nil, "signalserver:port", "shared-uuid", "target-ip")
    if err != nil {
        log.Fatal(err)
    }
    
    udpConn, err := client.PunchUDPHole(resp, 300, 10)
    if err != nil {
        log.Fatal(err)
    }
    defer udpConn.Close()
    
    // Use the P2P UDP connection
    udpConn.Write([]byte("Hello UDP P2P!"))
}
```


### Server 

#### `server.Start(address string) error`
Starts the signaling server on the specified address. Includes both TCP signaling and UDP STUN server.

## Usage Patterns

### Initiator Pattern (Waits for receiver)
```go
// TCP Initiator
resp, err := client.GetTCPPeer(nil, serverAddr, uuid, targetIP)
if err != nil {
    return err
}
conn, err := client.PuncTCPhHole(resp, nil, 300, 10)

// UDP Initiator  
resp, err := client.GetUDPPeer(nil, serverAddr, uuid, targetIP)
if err != nil {
    return err
}
udpConn, err := client.PunchUDPHole(resp, 300, 10)
```

### Receiver Pattern (Checks for waiting initiators)
```go
resp, err := client.GetClientPeer(nil, serverAddr, uuid, "")
if err != nil {
    return err
}

if resp.Protocol == "tcp" {
    conn, err := client.PuncTCPhHole(resp, nil, 300, 10)
    // Handle TCP connection
} else if resp.Protocol == "udp" {
    udpConn, err := client.PunchUDPHole(resp, 300, 10)
    // Handle UDP connection
}
```

## Running the Examples

### Start a Signaling Server
```bash
cd server_test
go run test.go 0.0.0.0:8080
```

### Test TCP Connection

**Terminal 1 (Initiator):**
```bash
cd client_test
go run test.go localhost:8080 shared-uuid tcp 192.168.1.100
```

**Terminal 2 (Receiver):**
```bash
cd client_test  
go run test.go localhost:8080 shared-uuid
```

### Test UDP Connection

**Terminal 1 (Initiator):**
```bash
cd client_test
go run test.go localhost:8080 shared-uuid udp 192.168.1.100
```

**Terminal 2 (Receiver):**
```bash
cd client_test
go run test.go localhost:8080 shared-uuid
```

## How It Works

1. **Signaling Phase**: Both peers connect to a central signaling server to exchange connection information
2. **STUN Discovery** (UDP only): Clients discover their public IP and port using the built-in STUN server
3. **Hole Punching**: Peers attempt simultaneous connections to each other's public endpoints
4. **Connection Establishment**: First successful connection wins, others are discarded

### TCP Hole Punching
- Uses socket reuse (SO_REUSEADDR/SO_REUSEPORT) for better NAT traversal
- Attempts both outbound connections and inbound listening simultaneously
- Configurable retry count and timeout

### UDP Hole Punching  
- Leverages built-in STUN server for public endpoint discovery
- Uses ping/pong verification to confirm hole punching success
- More reliable than TCP in most NAT scenarios

## Advanced Configuration

### Custom Dialer
```go
dialer := &net.Dialer{
    Timeout: 30 * time.Second,
    LocalAddr: &net.TCPAddr{IP: net.ParseIP("192.168.1.100")},
}

resp, err := client.GetTCPPeer(dialer, serverAddr, uuid, targetIP)
```

### Timeout and Retry Configuration
```go
// More aggressive settings for difficult networks
conn, err := client.PuncTCPhHole(resp, nil, 500, 30) // 500 tries, 30 second timeout
udpConn, err := client.PunchUDPHole(resp, 500, 30)
```
## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

## License
This project is licensed under the terms included in the LICENSE file.

