package main

import (
	"os"

	"github.com/tunnels-is/stunturn/server"
)

func main() {
	_ = server.Start(os.Args[1])
}
