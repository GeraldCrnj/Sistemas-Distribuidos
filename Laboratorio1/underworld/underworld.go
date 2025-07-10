package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"time"

	pb "underworld/proto/grpc-server/proto"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedBountyhuntersUnderworldServiceServer
}

/* Determina si el submundo enviará mercenarios a recuperar un pirata capturado de nivel alto. */
func (s *server) IsUnderworldSendingMercenaries(ctx context.Context, req *pb.PirateRequest) (*pb.SendingMercenariesResponse, error) {
	isSendingMercenaries := false

	// Si el pirata capturado es alto, el submundo tiene un 35% de probabilidad de enviar mercenarios a recuperar al pirata
	if req.PirateCatched.DangerLevel == "Alto" && rand.Float64() < 0.35 {
		isSendingMercenaries = true
		fmt.Printf("Mercenarios: El submundo envía mercenarios a recuperar al pirata %s.\n", req.PirateCatched.Name)
	}

	return &pb.SendingMercenariesResponse{
		IsSendingMercenaries: isSendingMercenaries,
	}, nil
}

/* Vende un pirata al submundo con riesgo de fraude y posible pago entre el 100% y 150% de la recompensa. */
func (s *server) SellPirate(ctx context.Context, req *pb.SellUnderworldRequest) (*pb.SellUnderworldResponse, error) {
	// Probabilidad de fraude
	if rand.Float64() < 0.35 {
		fmt.Printf("Entrega fraudulenta: Cometemos fraude y no pagamos por el pirata %s.\n", req.Pirate.Name)
		return &pb.SellUnderworldResponse{
			Payment: 0,
			IsFraud: true,
		}, nil
	}

	// Si no existe fraude, se calcula el pago del 100%-150% de la recompensa
	reward := req.Pirate.Reward
	paymentRate := 1 + rand.Float64()*0.5
	underworldPayment := int64(paymentRate * float64(reward))
	fmt.Printf("Entrega aprobada: Pagamos %d por el pirata %s de recompensa %d.\n", underworldPayment, req.Pirate.Name, req.Pirate.Reward)
	return &pb.SellUnderworldResponse{
		Payment: underworldPayment,
		IsFraud: false,
	}, nil
}

const (
	timeOperations = 60 * 6 * time.Second // Tiempo de operación total
)

func main() {

	// Inicializa la semilla aleatoria
	rand.Seed(time.Now().UnixNano())

	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Error al escuchar el puerto 50052: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterBountyhuntersUnderworldServiceServer(grpcServer, &server{})

	// Crea un thread (hilo) que cierra el programa luego de pasar el tiempo de operaciones
	go func() {
		time.Sleep(timeOperations)
		fmt.Println("Tiempo expirado: Cerrando conexiones...")

		grpcServer.GracefulStop()

		fmt.Println("Conexiones cerradas. Finalizando programa.")
		os.Exit(0)
	}()

	fmt.Println("Servidor grpc corriendo en el puerto 50052")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Error al escuchar el puerto 50052: %v", err)
	}

	defer grpcServer.Stop()
}
