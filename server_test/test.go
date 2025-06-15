package main

import (
	"os"

	"github.com/zveinn/stunturn/server"
)

func main() {
	server.Start(os.Args[1])
}
