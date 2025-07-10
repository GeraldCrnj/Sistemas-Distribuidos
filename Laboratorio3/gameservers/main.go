package main

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	if len(os.Args) < 3 {
		fmt.Println("Uso: ./gameserver <server-id> <port>")
		os.Exit(1)
	}

	serverID := os.Args[1]
	portStr := os.Args[2]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		fmt.Printf("Puerto inválido: %s\n", portStr)
		os.Exit(1)
	}

	serverName := fmt.Sprintf("server-%s", serverID)
	startGameServer(serverName, port)

	select {}
}
