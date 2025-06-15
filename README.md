# Replace YOUR_SIGNAL_SERVER_IP with the actual IP or domain of your signal server
./peer-client -id PeerA -signal ws://YOUR_SIGNAL_SERVER_IP:8080/ws

./peer-client -id PeerB -connect PeerA -signal ws://YOUR_SIGNAL_SERVER_IP:8080/ws
