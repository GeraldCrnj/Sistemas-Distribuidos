package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	pb "main.go/proto/compiled"
)

func startGameServer(id string, port int) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("No se pudo iniciar servidor %s: %v", id, err)
	}

	grpcServer := grpc.NewServer()
	gs := NewGameServer(id, port)

	pb.RegisterGameServersServiceServer(grpcServer, gs)

	go func() {
		log.Printf("Servidor %s escuchando en :%d", id, port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Servidor %s falló al servir: %v", id, err)
		}
	}()

	// Intenta registrar el servidor como AVAILABLE con retries
	go func() {
		for {
			err := gs.updateMatchmakerStatus("AVAILABLE")
			if err != nil {
				log.Printf("Error registrando server %s, reintentando: %v", id, err)
				time.Sleep(1 * time.Second)
				continue
			}
			break
		}
	}()

	// Simulación de caídas aleatorias
	startFailureSimulation(gs)
}
