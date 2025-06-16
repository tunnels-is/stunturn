# Tunnels NAT Penetrator
The tunnels NAT penetrator pairs `initiators` and `receivers` using an `access_key`.

## todo
 - add configurable timeouts
 - add more (outside module) error flow control

## Small note about the implementation
We deviced to use the concepts of `initiator` and `receiver` in order to enable greater horizontal scaling.</br>
The `initiator` will open a connection and keep it open until a matching `receiver` checks in (30 second timeout).</br>
The `receiver` uses a standard HTTP request to check if any `initiators` are waiting (exits instantly if no `initiators` are waiting).</br>
Doing it this way prevents socket buildup on both sides of the signaling process.

## Public signaling server comming soon
We will be launching a public signaling server @ signal.tunnels.is this week (free of use).

## Launching your own server
You can use the 'server_test' code to launch your own server
```bash
$ ./server ip:port
```
 or embed the server using the 'server' module.
```go
import "github.com/zveinn/stunturn/server"
err := server.Start("x.x.x.x:xxxx")
```

## Basic usage
```go
import "github.com/zveinn/stunturn/client"

// These methods open a long-lived connection that waits for a peer
resp, err := client.GetTCPPeer(serverAddr, uuid, targetIP)
p2pConn, err := client.PuncTCPhHole(resp)

resp, err := client.GetTCPPeer(serverAddr, uuid, targetIP)
p2pConn, err := client.PunchUDPHole(resp)


// This make a single HTTP call to the signaling server to check if any clients are waiting
 resp, err := client.GetClientPeer(serverAddr, access_key, targetIP)
if resp.Protocol == "tcp"{
 p2pConn, err := client.PuncTCPhHole(resp)
} else if resp.Protocol == "udp"{
 p2pConn, err := client.PunchUDPHole(resp)
}
```

## Server
Folder location: server_test
```bash
$ ./server [signal_server_address]

Example: ./server 11.11.11.11:1000
```

## Initiator
Folder location: client</br>
The `initiator` will open and hold a connection to the signal server until a `receiver` calls in and they get matched.</br>
NOTE: initiator and receiver `access_key` must match.
```bash
$ ./client [signal_server_address] [access_key] [protocol] [server_ip]

Example: ./client 11.11.11.11:1000 randomaccesskey tcp 12.12.12.12
```

## Receiver (ip: 12.12.12.12)
Folder location: client</br>
The receiver does not care about the protocol or an incomming IP.</br>
It makes a single HTTP request to check if there are `initiators` waiting to connect to it.</br>
NOTE: initiator and receiver `access_key` must match.
```bash
$ ./client [signal_server_address] [access_key]

Example: ./client 11.11.11.11:1000 randomaccesskey
```

## Protocols supported
 - tcp
 - udp
