package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"google.golang.org/grpc"
	pb "main.go/proto/compiled"
)

type GameServer struct {
	pb.UnimplementedGameServersServiceServer
	ID     string
	Port   int
	Status string

	mu     sync.Mutex
	client pb.MatchmakerServiceClient
	conn   *grpc.ClientConn
}

func NewGameServer(id string, port int) *GameServer {
	conn, err := grpc.Dial("matchmaker_container:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("fallo conexión matchmaker: %v", err)
	}
	client := pb.NewMatchmakerServiceClient(conn)
	return &GameServer{
		ID:     id,
		Port:   port,
		client: client,
		conn:   conn,
	}
}

func (s *GameServer) Close() {
	s.conn.Close()
}

// updateMatchmakerStatus informa al Matchmaker del estado actual del servidor.
func (s *GameServer) updateMatchmakerStatus(newStatus string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var serverID int32
	_, err := fmt.Sscanf(s.ID, "server-%d", &serverID)
	if err != nil {
		return fmt.Errorf("ID inválido %s: %w", s.ID, err)
	}

	resp, err := s.client.UpdateServerStatus(ctx, &pb.ServerStatusUpdateRequest{
		ServerId:  serverID,
		NewStatus: newStatus,
		Address:   fmt.Sprintf("gameservers_container:%d", s.Port),
	})
	if err != nil {
		return fmt.Errorf("error UpdateServerStatus RPC: %w", err)
	}

	if resp.StatusCode != 0 {
		return fmt.Errorf("UpdateServerStatus fallo con código %d", resp.StatusCode)
	}

	s.mu.Lock()
	s.Status = newStatus
	s.mu.Unlock()

	log.Printf("[%s] actualizó estado a %s en matchmaker", s.ID, newStatus)
	return nil
}

func (s *GameServer) AssignMatch(ctx context.Context, req *pb.AssignMatchRequest) (*pb.AssignMatchResponse, error) {
	log.Printf("GameServer %s recibió una nueva partida: %s con jugadores: %v", s.ID, req.MatchId, req.PlayerIds)

	// Informar al matchmaker que está ocupado
	if err := s.updateMatchmakerStatus("BUSY"); err != nil {
		log.Printf("Error actualizando estado a BUSY: %v", err)
	}

	go func(serverID string, matchID string, players []string) {
		rand.Seed(time.Now().UnixNano())

		// Duración aleatoria entre 10 y 20 segundos
		duration := time.Duration(10+rand.Intn(11)) * time.Second
		log.Printf("[%s] inició partida %s. Duración: %s", s.ID, matchID, duration)

		time.Sleep(duration)

		log.Printf("[%s] finalizó partida %s", s.ID, matchID)

		// Informar al matchmaker que está disponible nuevamente
		if err := s.updateMatchmakerStatus("AVAILABLE"); err != nil {
			log.Printf("Error actualizando estado a AVAILABLE: %v", err)
		}

		var id int32
		_, err := fmt.Sscanf(serverID, "server-%d", &id)
		if err != nil {
			log.Printf("Error parseando serverID %s: %v", serverID, err)
			return
		}

		req := &pb.UpdatePlayersRequest{
			ServerId:   id,
			PlayersIds: players,
		}

		resp, err := s.client.UpdatePlayersStatus(context.Background(), req)
		if err != nil {
			log.Printf("Error llamando UpdatePlayersStatus: %v", err)
			return
		}

		if resp.StatusCode != 0 {
			log.Printf("Error en respuesta UpdatePlayersStatus: %s", resp.Message)
			return
		}

		log.Printf("Estados de jugadores actualizados exitosamente para la partida %s", matchID)

	}(s.ID, req.MatchId, req.PlayerIds)

	return &pb.AssignMatchResponse{
		StatusCode: 0,
		Message:    fmt.Sprintf("Partida %s asignada exitosamente", req.MatchId),
	}, nil
}

func (s *GameServer) PingServer(ctx context.Context, req *pb.ServerId) (*pb.PingResponse, error) {
	log.Printf("Ping recibido en GameServer %s", s.ID)
	return &pb.PingResponse{
		StatusCode: 0,
	}, nil
}
