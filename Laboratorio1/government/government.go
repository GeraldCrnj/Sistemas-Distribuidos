package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	pb "government/proto/grpc-server/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Bountyhunter struct {
	MarineSells     int32
	UnderworldSells int32
}

type server struct {
	pb.UnimplementedBountyhuntersgovernmentServer
	pb.UnimplementedGovernmentMarineServer
	pirates                 []*pb.Pirate           // Lista de piratas
	bountyhuntersReputation map[int32]int32        // Historial de reputación de cazarrecompensas
	bountyHuntersSells      map[int32]Bountyhunter // Registro de ventas de cada cazarrecompensas
	soldPiratesToMarine     int32
	soldPiratesToUnderworld int32
}

/* Revisa si el ratio de ventas al submundo supera el 40% del total de ventas de un cazarrecompensas en específico */
func (s *server) CheckIfTooManySellsToUnderworld(ctx context.Context, req *pb.TooManySellsRequest) (*pb.TooManySellsResponse, error) {
	tooManySells := false

	// Consulta el registro de ventas del cazarrecompensas
	sells, ok := s.bountyHuntersSells[req.BountyHunterId]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "No se encontró el registro de ventas del cazarrecompensas con ID: %d", req.BountyHunterId)
	}

	// Si las ventas del submundo son mayor al 40% del total de ventas del cazarrecompensas, se considera que es mucha venta ilegal
	totalSells := sells.MarineSells + sells.UnderworldSells
	if totalSells > 0 {
		ratio := float64(sells.UnderworldSells) / float64(totalSells)
		if ratio > 0.4 {
			tooManySells = true
		}
	}

	return &pb.TooManySellsResponse{
		TooManySells: tooManySells,
	}, nil
}

/* Función para obtener la reputación del cazarrecompensas que intenta vender un pirata a la marina, por lo que la marina le consulta al gobierno si es de fiar */
func (s *server) GetBountyHunterReputation(ctx context.Context, req *pb.BountyHunterReputationRequest) (*pb.BountyHunterReputationResponse, error) {
	reputation, ok := s.bountyhuntersReputation[req.BountyHunterId]
	if !ok {
		reputation = 50
	}

	return &pb.BountyHunterReputationResponse{
		BountyHunterReputation: reputation,
	}, nil
}

/* Retorna la lista completa de piratas buscados desde el gobierno. */
func (s *server) GetWantedPirates(ctx context.Context, req *pb.GetWantedPiratesRequest) (*pb.GetWantedPiratesResponse, error) {
	return &pb.GetWantedPiratesResponse{
		Pirates: getWantedPirates(s.pirates),
	}, nil
}

/* Retorna la lista de piratas buscados solicitada por la marina. */
func (s *server) GetWantedPiratesFromMarine(ctx context.Context, req *pb.GetWantedPiratesFromMarineRequest) (*pb.GetWantedPiratesFromMarineResponse, error) {
	return &pb.GetWantedPiratesFromMarineResponse{
		Pirates: getWantedPirates(s.pirates),
	}, nil
}

/* Actualiza la reputación del cazarrecompensas según el destino del pirata y decide si alertar a la marina por exceso de ventas al submundo. */
func (s *server) SendCaptureReport(ctx context.Context, req *pb.SendCaptureReportRequest) (*pb.SendCaptureReportResponse, error) {
	reputation, ok := s.bountyhuntersReputation[req.BountyhunterId]
	if !ok {
		reputation = 50 // Si no existe una reputación guardada, esta es la vez para inicializarlo
	}

	switch {
	//En caso de que se pierda o lo rescate el submundo
	case req.State == "Perdido":
		// Reputación baja
		reputation -= 5
		fmt.Printf("INFORME: Se recibe informe de captura del pirata %s PERDIDO por cazarrecompensas %d.\n", req.Pirate.Name, req.BountyhunterId)

	case req.State == "Entregado" && req.Buyer == "Marina":
		// Reputación debe subir
		reputation += 10
		s.soldPiratesToMarine += 1
		saveLastSellDestiny("Marina")

		// Añade 1 a las ventas de la marina
		s.incrementSells(req.BountyhunterId, "Marina")
		err := s.modifyPirateState(req.Pirate.Id, "Capturado")
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "No se encontró el pirata con ID %d", req.Pirate.Id)
		}

		fmt.Printf("INFORME: Se recibe informe de captura del pirata %s ENTREGADO por cazarrecompensas %d a la MARINA.\n", req.Pirate.Name, req.BountyhunterId)

	case req.State == "Entregado" && req.Buyer == "Submundo":
		// Reputación debe bajar
		reputation -= 5
		s.soldPiratesToUnderworld += 1
		saveLastSellDestiny("Submundo")

		// Añade 1 a las ventas del submundo
		s.incrementSells(req.BountyhunterId, "Submundo")
		err := s.modifyPirateState(req.Pirate.Id, "Capturado")
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "No se encontró el pirata con ID %d", req.Pirate.Id)
		}

		fmt.Printf("INFORME: Se recibe informe del pirata %s ENTREGADO por cazarrecompensas %d al SUBMUNDO.\n", req.Pirate.Name, req.BountyhunterId)
	}

	// Se definen los limites de la reputación del cazarrecompensas (0-100)
	if reputation > 100 {
		reputation = 100
	} else if reputation < 0 {
		reputation = 0
	}

	// Guardamos el nuevo valor de la reputación
	s.bountyhuntersReputation[req.BountyhunterId] = reputation

	// Se comprueba si se debe alertar a la marina o no
	alerted := false
	totalSold := s.soldPiratesToMarine + s.soldPiratesToUnderworld
	if totalSold > 0 && alertsToSkip == 0 {
		ratio := float64(s.soldPiratesToUnderworld) / float64(totalSold)
		if ratio > 0.4 {
			// Si el ratio de ventas al submundo es mayor al 40%, se alerta a la marina
			s.alertMarine()
		}
	}

	if !alerted && alertsToSkip > 0 {
		alertsToSkip -= 1
	}

	return &pb.SendCaptureReportResponse{
		NewReputation: reputation,
	}, nil
}

/* Busca un pirata por su ID dentro de una lista y le cambia el estado (state) */
func (s *server) modifyPirateState(id int32, newState string) error {
	for _, pirate := range s.pirates {
		if pirate.Id == id {
			pirate.State = newState
			return nil
		}
	}
	return fmt.Errorf("Pirata con ID %d no encontrado", id)
}

/* Envia el informe de la marina al gobierno */
func (s *server) SendMarineCaptureReport(ctx context.Context, req *pb.SendMarineCaptureReportRequest) (*pb.SendMarineCaptureReportResponse, error) {
	// Cambia el estado del pirata a capturado
	for _, pirate := range s.pirates {
		if pirate.Id == req.Pirate.Id && pirate.State == "Buscado" {
			pirate.State = "Capturado"
		}
	}

	// Se actualiza la reputación del cazarrecompensas
	s.bountyhuntersReputation[req.BountyHunterId] -= 5

	fmt.Printf("INFORME: Se recibe informe del pirata %s CONFISCADO por parte de la MARINA al cazarrecompensas %d.\n", req.Pirate.Name, req.BountyHunterId)

	return &pb.SendMarineCaptureReportResponse{}, nil
}

/* Esta función incrementa el contador de ventas de un cazarrecompensas hacia la Marina o el Submundo, según el destino especificado. */
func (s *server) incrementSells(bountyHunterId int32, destiny string) {
	sells := s.bountyHuntersSells[bountyHunterId]

	if destiny == "Marina" {
		sells.MarineSells += 1
	} else {
		sells.UnderworldSells += 1
	}

	s.bountyHuntersSells[bountyHunterId] = sells
}

/* Función que se conecta y alerta a la marina de que hay muchas ventas en el submundo */
func (s *server) alertMarine() {
	marineConn, err := grpc.Dial("marine_container:50053", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("no se pudo conectar con la marina: %v", err)
	}
	defer marineConn.Close()

	marineClient := pb.NewGovernmentMarineClient(marineConn)

	_, err = marineClient.AlertMarine(context.Background(), &pb.AlertMarineRequest{})
	if err != nil {
		log.Fatalf("Hubo un problema al alertar a la marina %v", err)
	}

	fmt.Printf("¡ALERTA!: Demasiadas ventas al SUBMUNDO, la MARINA fue contactada.\n")

	// Entendemos capturas recientes por las últimas 5 capturas
	majorityDestiny := getMajorityDestiny()
	rate := float64(1)
	if majorityDestiny == "Marina" {
		rate -= 0.2
	} else {
		rate += 0.2
	}

	// Ajuste dinámico de recompensas por capturas recientes y actividad ilegal
	penalty := int32(1000 * rate)
	wantedPirates := getWantedPirates(s.pirates)
	for _, pirate := range wantedPirates {
		pirate.Reward = int32(pirate.Reward - penalty)
	}
	fmt.Printf("Ajuste dinámico de recompensas: Disminuye -%d berries las recompensas.\n", penalty)

	// Si se emitió una alerta reciente, entonces se alertará luego de que se envien 5 informes
	alertsToSkip = 5
}

/* Retorna el destino más usado en las últimas 5 ventas */
func getMajorityDestiny() string {
	counter := 0

	for _, destiny := range lastSellsDestiny {
		if destiny == "Marina" {
			counter += 1
		} else {
			counter -= 1
		}
	}

	if counter >= 0 {
		return "Marina"
	}

	return "Submundo"
}

/* Carga la lista de piratas completa desde el csv */
func loadPiratesFromCsv(path string) ([]*pb.Pirate, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el archivo: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var wantedPirates []*pb.Pirate
	line := 0

	for scanner.Scan() {
		lineText := scanner.Text()

		if line == 0 {
			line++
			continue
		}

		fields := strings.Split(lineText, ",")
		if len(fields) != 5 {
			fmt.Println("Línea inválida:", lineText)
			continue
		}

		id, err1 := strconv.Atoi(fields[0])
		reward, err2 := strconv.Atoi(fields[2])

		if err1 != nil || err2 != nil {
			fmt.Printf("Error al convertir números en línea %d\n", line)
			continue
		}

		pirata := &pb.Pirate{
			Id:          int32(id),
			Name:        fields[1],
			Reward:      int32(reward),
			DangerLevel: fields[3],
			State:       fields[4],
		}

		wantedPirates = append(wantedPirates, pirata)
		line++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error al leer el archivo: %v", err)
	}

	return wantedPirates, nil
}

/* Obtiene la lista de piratas buscados a partir de la lista general */
func getWantedPirates(pirates []*pb.Pirate) []*pb.Pirate {
	var filteredPirates []*pb.Pirate
	for _, pirate := range pirates {
		if pirate.State == "Buscado" {
			filteredPirates = append(filteredPirates, pirate)
		}
	}
	return filteredPirates
}

/* Recibe entity: "Submundo" o "Marina" para guardar las 5 últimas ventas */
func saveLastSellDestiny(entity string) {
	lastSellsDestiny = append(lastSellsDestiny, entity)
	if len(lastSellsDestiny) > 5 {
		lastSellsDestiny = lastSellsDestiny[1:]
	}
}

/* Asigna un ID a cada cazarrecompensas después de iniciar la conexión con el gobierno */
func (s *server) AssignBountyhunterId(ctx context.Context, req *pb.AssignBountyhunterIdRequest) (*pb.AssignBountyhunterIdResponse, error) {
	activeBountyhunters += 1
	bountyHunterId := int32(activeBountyhunters)

	// Asigna los valores iniciales de las ventas del cazarrecompensas
	s.bountyHuntersSells[bountyHunterId] = Bountyhunter{
		MarineSells:     0,
		UnderworldSells: 0,
	}

	// Asigna en 50 la reputación inicial del cazarrecompensas
	s.bountyhuntersReputation[bountyHunterId] = 50

	fmt.Printf("REGISTRO: Se ha registrado al cazarrecompensas %d.\n", bountyHunterId)

	return &pb.AssignBountyhunterIdResponse{
		BountyhunterId: bountyHunterId,
	}, nil
}

var (
	alertsToSkip        = 0
	lastSellsDestiny    []string
	activeBountyhunters = 0
)

const (
	timeOperations = 60 * 6 * time.Second // Tiempo de operación total
)

func main() {
	time.Sleep(5 * time.Second)

	pirates, err := loadPiratesFromCsv("piratas_grande.csv")
	if err != nil {
		log.Fatalf("Error al cargar la lista inicial de piratas en la memoria: %v", err)
	}

	for _, pirate := range pirates {
		fmt.Printf("Pirata: %+v\n", pirate)
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Error al escuchar el puerto 50051: %v", err)
	}

	grpcServer := grpc.NewServer()
	s := &server{
		pirates:                 pirates,
		bountyhuntersReputation: make(map[int32]int32),
		bountyHuntersSells:      make(map[int32]Bountyhunter),
	}

	pb.RegisterBountyhuntersgovernmentServer(grpcServer, s)
	pb.RegisterGovernmentMarineServer(grpcServer, s)

	// Crea un thread (hilo) que cierra el programa luego de pasar el tiempo de operaciones
	go func() {
		time.Sleep(timeOperations)
		fmt.Println("Tiempo expirado: Cerrando conexiones...")

		grpcServer.GracefulStop()

		fmt.Println("Conexiones cerradas. Finalizando programa.")
		os.Exit(0)
	}()

	fmt.Printf("Servidor grpc corriendo en el puerto 50051\n\n")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Error al escuchar el puerto 50051: %v", err)
	}

	defer grpcServer.Stop()
}
