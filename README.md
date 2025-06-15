# Tunnels NAT Penetrator
The tunnels NAT penetrator pairs `initiators` and `receivers` using an `access_key`.

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
