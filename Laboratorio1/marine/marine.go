package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"time"

	pb "marine/proto/grpc-server/proto"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedBountyhuntersMarineServiceServer
	pb.UnimplementedGovernmentMarineServer
	governmentClient pb.GovernmentMarineClient
}

/* Busca un pirata por su ID dentro de una lista. Retorna el pirata si lo encuentra, o nil si no existe. */
func findPirateById(pirates []*pb.Pirate, id int32) *pb.Pirate {
	for _, pirate := range pirates {
		if pirate.Id == id {
			return pirate
		}
	}
	return nil // No encontrado
}

/*
Procesa la venta de un pirata a la Marina:
Verifica si el pirata aún está buscado y si el cazarrecompensas tiene suficiente reputación.
Puede rechazar la venta si el pirata ya fue entregado o si la reputación es baja.
*/
func (s *server) SellPirate(ctx context.Context, req *pb.SellMarineRequest) (*pb.SellMarineResponse, error) {
	// Contactar al gobierno, del gobierno se recibe la lista, esa lista te dará el state del pirata
	// y si sigue estando buscado, se paga por el, si no se rechaza

	respPirates, err := s.governmentClient.GetWantedPiratesFromMarine(ctx, &pb.GetWantedPiratesFromMarineRequest{})
	if err != nil {
		log.Fatalf("Ocurrió un problema al consultar la lista de piratas buscados al gobierno: %v", err)
	}

	// El pirata no está en la lista de buscados, ya fue capturado, entonces la marina no paga
	pirateFound := findPirateById(respPirates.Pirates, req.Pirate.Id)
	if pirateFound == nil {
		fmt.Printf("Entrega rechazada del pirata '%s' (Cazarrecompensas %d): El pirata ya no está en la lista de buscados.\n", req.Pirate.Name, req.BountyhunterId)
		return &pb.SellMarineResponse{
			StatusCode: int32(3), // 3: Rechazo por pirata ya entregado
			Payment:    int32(0),
		}, nil
	}

	// Consulta al gobierno por los datos del cazarrecompensas y ver si su reputacion es menor a 15 o no
	respReputation, err := s.governmentClient.GetBountyHunterReputation(ctx, &pb.BountyHunterReputationRequest{
		BountyHunterId: req.BountyhunterId,
	})
	if err != nil {
		log.Fatalf("Ocurrió un problema al consultar al gobierno por la reputación del cazarrecompensas: %v", err)
	}

	/* Si la reputación es menor a 15, establecemos que puede o no rechazar su entrega */
	if respReputation.BountyHunterReputation < 15 && rand.Float64() < 0.5 {
		fmt.Printf("Entrega rechazada del pirata '%s' (Cazarrecompensas %d): Baja reputación del cazarrecompensas.\n", req.Pirate.Name, req.BountyhunterId)
		return &pb.SellMarineResponse{
			StatusCode: int32(2), // 2: Rechazo por reputación
			Payment:    int32(0),
		}, nil
	}

	// Se revisa si el cazarrecompensas ha vendido mucho al submundo
	respActivityUnderworld, err := s.governmentClient.CheckIfTooManySellsToUnderworld(ctx, &pb.TooManySellsRequest{BountyHunterId: req.BountyhunterId})
	if err != nil {
		log.Fatalf("Ocurrió un problema al consultar al gobierno si el cazarrecompensas ha hecho mucha venta ilegal: %v", err)
	}

	if respActivityUnderworld.TooManySells {
		fmt.Printf("Entrega aprobada del pirata '%s' (Cazarrecompensas %d): Pago reducido a la mitad por sobreventa del cazarrecompensas al submundo.\n", req.Pirate.Name, req.BountyhunterId)
		return &pb.SellMarineResponse{
			StatusCode: int32(1), // 1: Pago reducido por sobreactividad
			Payment:    int32(float64(req.Pirate.Reward) * 0.5),
		}, nil
	}

	fmt.Printf("Entrega aprobada del pirata '%s' (Cazarrecompensas %d): Se paga la totalidad al cazarrecompensas.\n", req.Pirate.Name, req.BountyhunterId)
	return &pb.SellMarineResponse{
		StatusCode: int32(0), // 0: Éxito
		Payment:    int32(req.Pirate.Reward),
	}, nil
}

/*
Informa si la Marina confiscará un pirata capturado.
Esto ocurre solo si hay redada activa y el pirata es de nivel alto.
*/
func (s *server) IsMarineConfiscatingPirate(ctx context.Context, req *pb.MarineConfiscatingRequest) (*pb.MarineConfiscatingResponse, error) {
	// La marina convoca a una redada si las ventas del submundo son muy elevadas
	// Para esto, el gobierno le emite una alerta a la marina
	// Se dará o no el evento de redada

	// La redada ocurre si el pirata es de alto nivel, con un 25% de probabilidad (Ver README: Consideraciones (2))
	isMarineConfiscating := false
	if piratesToConfiscate > 0 && req.PirateCatched.DangerLevel == "Alto" && rand.Float64() < 0.25 {
		piratesToConfiscate -= 1
		isMarineConfiscating = true
		fmt.Printf("REDADA: Debido a la actividad ilegal y debido al alto nivel de peligrosidad del pirata %s se confisca al cazarrecompensas %d.\n", req.PirateCatched.Name, req.BountyHunterId)
	} else {
		return &pb.MarineConfiscatingResponse{
			IsMarineConfiscating: isMarineConfiscating,
		}, nil
	}

	// Informe al gobierno
	_, err := s.governmentClient.SendMarineCaptureReport(ctx, &pb.SendMarineCaptureReportRequest{
		BountyHunterId: req.BountyHunterId,
		Pirate:         req.PirateCatched,
	})
	if err != nil {
		log.Printf("Ocurrió un error al enviar el informe de captura desde la marina al gobierno. \n")
	}
	fmt.Printf("INFORME: Se ha enviado el informe de captura del pirata %s al GOBIERNO.\n", req.PirateCatched.Name)

	return &pb.MarineConfiscatingResponse{
		IsMarineConfiscating: isMarineConfiscating,
	}, nil
}

/* Usada por el gobierno para alertar a la marina */
func (s *server) AlertMarine(ctx context.Context, req *pb.AlertMarineRequest) (*pb.AlertMarineResponse, error) {
	// Si se alerta a la marina, la marina establecerá que habrán X piratas a confiscar
	// En caso de alertarse cuando siguen en redada (piratas a confiscar > 0) no se hace nada
	if piratesToConfiscate == 0 {
		piratesToConfiscate = 3

		// Las redadas funcionan cuando el pirata es de alta peligrosidad, con probabilidad del 25% de redada
		fmt.Printf("ALERTA RECIBIDA: Se ha recibido la alerta del gobierno. Redadas activadas a piratas peligrosos.\n")
	}

	return &pb.AlertMarineResponse{}, nil
}

var (
	piratesToConfiscate = 0
)

const (
	timeOperations = 60 * 6 * time.Second // Tiempo de operación total
)

func main() {
	// Inicializa la semilla aleatoria
	rand.Seed(time.Now().UnixNano())

	lis, err := net.Listen("tcp", ":50053")
	if err != nil {
		log.Fatalf("Error al escuchar el puerto 50053: %v", err)
	}

	connGovernment, err := grpc.Dial("government_container:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("No se pudo conectar al gobierno: %v", err)
	}
	defer connGovernment.Close()

	governmentClient := pb.NewGovernmentMarineClient(connGovernment)

	grpcServer := grpc.NewServer()
	pb.RegisterBountyhuntersMarineServiceServer(grpcServer, &server{governmentClient: governmentClient})
	pb.RegisterGovernmentMarineServer(grpcServer, &server{governmentClient: governmentClient})

	// Crea un thread (hilo) que cierra el programa luego de pasar el tiempo de operaciones
	go func() {
		time.Sleep(timeOperations)
		fmt.Println("Tiempo expirado: Cerrando conexiones...")

		grpcServer.GracefulStop() // Apaga el servidor GRPC de forma segura
		connGovernment.Close()    // Cierra la conexión GRPC con el gobierno
		fmt.Println("Conexiones cerradas. Finalizando programa.")
		os.Exit(0)
	}()

	fmt.Println("Servidor GRPC corriendo en el puerto 50053")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Error al escuchar el puerto 50053: %v", err)
	}

	defer grpcServer.Stop()
}
