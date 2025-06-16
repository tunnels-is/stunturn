# Tunnels NAT Penetrator
The tunnels NAT penetrator pairs `initiators` and `receivers` using an `access_key`.

## Public signaling server comming soon
We will be launching a public signaling server @ signal.tunnels.is this week (free of use).

## Basic usage
```
import "github.com/zveinn/stunturn/client"

// This method open a long-lived connection that waits for a peer
resp, err := client.GetTCPPeer(serverAddr, uuid, targetIP)
if resp.Protocol == "tcp"{
 p2pConn, err := client.PuncTCPhHole(resp)
} else if resp.Protocol == "udp"{
 p2pConn, err := client.PunchUDPHole(resp)
}

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
```
$ ./server [signal_server_address]

Example: ./server 11.11.11.11:1000
```

## Initiator
Folder location: client
The `initiator` will open and hold a connection to the signal server until a `receiver` calls in and they get matched.
NOTE: initiator and receiver `access_key` must match.
```
$ ./client [signal_server_address] [access_key] [protocol] [server_ip]

Example: ./client 11.11.11.11:1000 randomaccesskey tcp 12.12.12.12
```

## Receiver (ip: 12.12.12.12)
Folder location: client
The receiver does not care about the protocol or an incomming IP.
It makes a single HTTP request to check if there are `initiators` waiting to connect to it.
```
$ ./client [signal_server_address] [access_key]

Example: ./client 11.11.11.11:1000 randomaccesskey
```

## Protocols supported
 - tcp
 - udp
