package main

import (
	"os"

	"github.com/tunnels-is/stunturn/server"
)

func main() {
	err := server.Start(os.Args[1])
	if err != nil {
		panic(err)
	}
}
