package main

import (
	"os"

	"github.com/zveinn/stunturn/server"
)

func main() {
	_ = server.Start(os.Args[1])
}
