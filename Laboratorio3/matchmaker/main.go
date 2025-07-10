package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	pb "main.go/proto/compiled"
)

type PlayerState struct {
	PlayerId       string
	State          string // "IDLE", "IN_QUEUE", "RESERVED", "IN_MATCH"
	MatchID        string
	ServerAddr     string
	VectorClock    map[string]int32
	QueueEnterTime time.Time
	GamePreference string
}

type GameServerState struct {
	ID             string
	Address        string
	Status         string // "AVAILABLE", "BUSY", "DOWN"
	LastCheck      time.Time
	CurrentMatchID string
}

type ActiveMatch struct {
	MatchID   string
	PlayerIDs []string
	ServerID  string
	StartTime time.Time
}

type MatchmakerServer struct {
	pb.UnimplementedMatchmakerServiceServer
	mu          sync.Mutex
	players     map[string]*PlayerState
	queues      map[string][]string // gameMode -> player IDs
	matchCtr    int
	vectorClock map[string]int32
	nodeID      string
	peers       []string
}

var (
	gameServers   = make(map[string]*GameServerState)
	activeMatches = make(map[string]*ActiveMatch)
)

func NewMatchmakerServer(nodeID string, peers []string) *MatchmakerServer {
	return &MatchmakerServer{
		players:     make(map[string]*PlayerState),
		queues:      make(map[string][]string),
		vectorClock: make(map[string]int32),
		nodeID:      nodeID,
		peers:       peers,
	}
}

func (s *MatchmakerServer) findAvailableGameServer() *GameServerState {
	for _, gs := range gameServers {
		if gs.Status == "AVAILABLE" {
			return gs
		}
	}
	return nil
}

func (s *MatchmakerServer) UpdateServerStatus(ctx context.Context, req *pb.ServerStatusUpdateRequest) (*pb.ServerStatusUpdateResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	serverID := fmt.Sprintf("server-%d", req.ServerId)
	server, exists := gameServers[serverID]
	if !exists {
		return &pb.ServerStatusUpdateResponse{
			StatusCode: 1,
		}, fmt.Errorf("servidor %d no encontrado", req.ServerId)
	}

	server.Status = req.NewStatus
	server.Address = req.Address
	server.LastCheck = time.Now()

	if req.NewStatus == "AVAILABLE" {
		server.CurrentMatchID = ""
	}

	log.Printf("Estado actualizado del servidor %s: %s en %s", server.ID, server.Status, server.Address)

	return &pb.ServerStatusUpdateResponse{
		StatusCode: 0,
	}, nil
}

func (s *MatchmakerServer) assignMatchToGameServer(server *GameServerState, matchID string, playerIDs []string) error {
	conn, err := grpc.Dial(server.Address, grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("error conectando al GameServer %s: %w", server.ID, err)
	}
	defer conn.Close()

	gameClient := pb.NewGameServersServiceClient(conn)

	resp, err := gameClient.AssignMatch(context.Background(), &pb.AssignMatchRequest{
		MatchId:   matchID,
		PlayerIds: playerIDs,
	})
	if err != nil {
		return fmt.Errorf("error en AssignMatch RPC: %w", err)
	}

	if resp.StatusCode != 0 {
		return fmt.Errorf("AssignMatch fallo con codigo %d", resp.StatusCode)
	}

	return nil
}

func (s *MatchmakerServer) QueuePlayer(ctx context.Context, req *pb.PlayerInfoRequest) (*pb.QueuePlayerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	player, exists := s.players[req.PlayerId]
	if !exists {
		player = &PlayerState{
			PlayerId:    req.PlayerId,
			State:       "IDLE",
			VectorClock: make(map[string]int32),
		}
		s.players[req.PlayerId] = player
	}

	if player.State == "IN_QUEUE" {
		return &pb.QueuePlayerResponse{
			StatusCode:  1,
			Message:     "Ya estás en cola",
			VectorClock: player.VectorClock,
		}, nil
	}

	if player.State == "IN_MATCH" {
		return &pb.QueuePlayerResponse{
			StatusCode:  2,
			Message:     "Ya estás en una partida",
			VectorClock: player.VectorClock,
		}, nil
	}

	player.State = "IN_QUEUE"
	player.VectorClock[req.PlayerId]++
	player.QueueEnterTime = time.Now()
	player.GamePreference = req.GameModePreference
	s.queues[req.GameModePreference] = append(s.queues[req.GameModePreference], req.PlayerId)

	return &pb.QueuePlayerResponse{
		StatusCode:  0,
		Message:     "Jugador en cola",
		VectorClock: player.VectorClock,
	}, nil
}

func (s *MatchmakerServer) AdminUpdateServerState(ctx context.Context, req *pb.AdminUpdateServerRequest) (*pb.AdminUpdateResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Buscar el servidor por ID
	serverKey := fmt.Sprintf("server-%d", req.ServerId)
	server, ok := gameServers[serverKey]
	if !ok {
		return &pb.AdminUpdateResponse{
			StatusCode: 1,
			Message:    fmt.Sprintf("Servidor con ID %d no encontrado", req.ServerId),
		}, nil
	}

	// Actualizar estado
	server.Status = req.NewForcedStatus
	server.LastCheck = time.Now()

	log.Printf("(!) Se ha forzado el estado del servidor server-%d a %s", req.ServerId, req.NewForcedStatus)
	return &pb.AdminUpdateResponse{
		StatusCode: 0,
		Message:    fmt.Sprintf("Estado del servidor %d actualizado a %s", req.ServerId, req.NewForcedStatus),
	}, nil
}

func (s *MatchmakerServer) GetPlayerStatus(ctx context.Context, req *pb.PlayerStatusRequest) (*pb.PlayerStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	player, exists := s.players[req.PlayerId]
	if !exists {
		return &pb.PlayerStatusResponse{
			Status:      "IDLE",
			VectorClock: make(map[string]int32),
		}, nil
	}

	return &pb.PlayerStatusResponse{
		Status:        player.State,
		MatchId:       player.MatchID,
		ServerAddress: player.ServerAddr,
		VectorClock:   player.VectorClock,
	}, nil
}

func (s *MatchmakerServer) matchLoop() {
	ticker := time.NewTicker(2 * time.Second)
	for range ticker.C {
		s.mu.Lock()

		type matchToAssign struct {
			server    *GameServerState
			matchID   string
			playerIDs []string
			p1        *PlayerState
			p2        *PlayerState
		}

		matches := []matchToAssign{}

		for mode, queue := range s.queues {
			newQueue := []string{}
			i := 0

			for i < len(queue)-1 {
				p1ID := queue[i]
				p2ID := queue[i+1]

				p1 := s.players[p1ID]
				p2 := s.players[p2ID]

				if p1.State != "IN_QUEUE" || p2.State != "IN_QUEUE" {
					if p1.State == "IN_QUEUE" {
						newQueue = append(newQueue, p1ID)
					}
					if p2.State == "IN_QUEUE" {
						newQueue = append(newQueue, p2ID)
					}
					i += 2
					continue
				}

				matchID := generateMatchID(s)
				server := s.findAvailableGameServer()
				if server == nil {
					log.Println("No hay servidores disponibles para asignar el match.")
					newQueue = append(newQueue, p1ID, p2ID)
					break
				}

				p1.State = "RESERVED"
				p2.State = "RESERVED"
				server.Status = "BUSY"
				server.LastCheck = time.Now()
				server.CurrentMatchID = matchID

				matches = append(matches, matchToAssign{
					server:    server,
					matchID:   matchID,
					playerIDs: []string{p1ID, p2ID},
					p1:        p1,
					p2:        p2,
				})

				i += 2
			}

			for ; i < len(queue); i++ {
				if s.players[queue[i]].State == "IN_QUEUE" {
					newQueue = append(newQueue, queue[i])
				}
			}
			s.queues[mode] = newQueue
		}

		s.mu.Unlock()

		for _, m := range matches {
			err := s.assignMatchToGameServer(m.server, m.matchID, m.playerIDs)
			s.mu.Lock()
			if err != nil {
				log.Printf("Error asignando partida: %v", err)
				m.p1.State = "IN_QUEUE"
				m.p2.State = "IN_QUEUE"
				m.server.Status = "AVAILABLE"
				m.server.CurrentMatchID = ""
			} else {
				m.p1.State = "IN_MATCH"
				m.p1.MatchID = m.matchID
				m.p1.ServerAddr = m.server.Address

				m.p2.State = "IN_MATCH"
				m.p2.MatchID = m.matchID
				m.p2.ServerAddr = m.server.Address

				activeMatches[m.matchID] = &ActiveMatch{
					MatchID:   m.matchID,
					PlayerIDs: m.playerIDs,
					ServerID:  m.server.ID,
					StartTime: time.Now(),
				}
				log.Printf("Match creado: %s con %s y %s en %s", m.matchID, m.playerIDs[0], m.playerIDs[1], m.server.Address)
			}
			s.mu.Unlock()
		}
	}
}

func (s *MatchmakerServer) CancelMatchmaking(ctx context.Context, req *pb.CancelRequest) (*pb.CancelResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	player, exists := s.players[req.PlayerId]
	if !exists {
		return &pb.CancelResponse{
			StatusCode:  1,
			Message:     "Jugador no existe.",
			VectorClock: map[string]int32{},
		}, nil
	}

	if player.State != "IN_QUEUE" {
		return &pb.CancelResponse{
			StatusCode:  2,
			Message:     "Jugador no está en cola.",
			VectorClock: player.VectorClock,
		}, nil
	}

	gameMode := player.GamePreference
	queue := s.queues[gameMode]
	newQueue := []string{}
	for _, id := range queue {
		if id != req.PlayerId {
			newQueue = append(newQueue, id)
		}
	}
	s.queues[gameMode] = newQueue

	player.State = "IDLE"
	player.MatchID = ""
	player.ServerAddr = ""
	player.VectorClock[req.PlayerId]++
	s.vectorClock[s.nodeID]++
	player.VectorClock = mergeVectorClocks(player.VectorClock, s.vectorClock)

	log.Printf("[Matchmaker] Jugador %s canceló el emparejamiento.", req.PlayerId)

	return &pb.CancelResponse{
		StatusCode:  0,
		Message:     "Emparejamiento cancelado.",
		VectorClock: player.VectorClock,
	}, nil
}

func generateMatchID(s *MatchmakerServer) string {
	s.matchCtr++
	return "match-" + time.Now().Format("150405") + fmt.Sprintf("-%d", s.matchCtr)
}

// --- Vector clock helpers ---

func mergeVectorClocks(vc1, vc2 map[string]int32) map[string]int32 {
	merged := make(map[string]int32)
	for k, v := range vc1 {
		merged[k] = v
	}
	for k, v := range vc2 {
		if current, exists := merged[k]; !exists || v > current {
			merged[k] = v
		}
	}
	return merged
}

func isIncomingNewer(vc1, vc2 map[string]int32) bool {
	for node, count2 := range vc2 {
		if count1, ok := vc1[node]; !ok || count2 > count1 {
			return true
		}
	}
	return false
}

func convertPlayerStateToProto(p *PlayerState) *pb.PlayerStateProto {
	return &pb.PlayerStateProto{
		PlayerId:           p.PlayerId,
		State:              p.State,
		MatchId:            p.MatchID,
		ServerAddr:         p.ServerAddr,
		VectorClock:        p.VectorClock,
		GamePreference:     p.GamePreference,
		QueueEnterTimeUnix: p.QueueEnterTime.Unix(),
	}
}

func convertProtoToPlayerState(p *pb.PlayerStateProto) *PlayerState {
	return &PlayerState{
		PlayerId:       p.PlayerId,
		State:          p.State,
		MatchID:        p.MatchId,
		ServerAddr:     p.ServerAddr,
		VectorClock:    p.VectorClock,
		GamePreference: p.GamePreference,
		QueueEnterTime: time.Unix(p.QueueEnterTimeUnix, 0),
	}
}

func convertGameServerStateToProto(gs *GameServerState) *pb.GameServerStateProto {
	return &pb.GameServerStateProto{
		Id:             gs.ID,
		Address:        gs.Address,
		Status:         gs.Status,
		LastCheckUnix:  gs.LastCheck.Unix(),
		CurrentMatchId: gs.CurrentMatchID,
	}
}

func convertProtoToGameServerState(gs *pb.GameServerStateProto) *GameServerState {
	return &GameServerState{
		ID:             gs.Id,
		Address:        gs.Address,
		Status:         gs.Status,
		LastCheck:      time.Unix(gs.LastCheckUnix, 0),
		CurrentMatchID: gs.CurrentMatchId,
	}
}

func (s *MatchmakerServer) SyncState(ctx context.Context, req *pb.StateSyncRequest) (*pb.StateSyncResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.vectorClock = mergeVectorClocks(s.vectorClock, req.VectorClock)

	for pid, pproto := range req.Players {
		localPlayer, exists := s.players[pid]
		if !exists {
			s.players[pid] = convertProtoToPlayerState(pproto)
			continue
		}
		mergedVC := mergeVectorClocks(localPlayer.VectorClock, pproto.VectorClock)
		if isIncomingNewer(localPlayer.VectorClock, pproto.VectorClock) {
			localPlayer.State = pproto.State
			localPlayer.MatchID = pproto.MatchId
			localPlayer.ServerAddr = pproto.ServerAddr
			localPlayer.GamePreference = pproto.GamePreference
			localPlayer.QueueEnterTime = time.Unix(pproto.QueueEnterTimeUnix, 0)
		}
		localPlayer.VectorClock = mergedVC
	}

	for sid, sproto := range req.Servers {
		localServer, exists := gameServers[sid]
		if !exists {
			gameServers[sid] = convertProtoToGameServerState(sproto)
			continue
		}
		if sproto.LastCheckUnix > localServer.LastCheck.Unix() {
			localServer.Status = sproto.Status
			localServer.Address = sproto.Address
			localServer.LastCheck = time.Unix(sproto.LastCheckUnix, 0)
			localServer.CurrentMatchID = sproto.CurrentMatchId
		}
	}

	s.vectorClock[s.nodeID]++

	return &pb.StateSyncResponse{
		StatusCode: 0,
		Message:    "Estado sincronizado exitosamente",
	}, nil
}

func (s *MatchmakerServer) syncWithPeer(peerAddress string) {
	conn, err := grpc.Dial(peerAddress, grpc.WithInsecure())
	if err != nil {
		log.Printf("Error conectando a peer %s: %v", peerAddress, err)
		return
	}
	defer conn.Close()

	client := pb.NewMatchmakerServiceClient(conn)

	s.mu.Lock()
	playersProto := make(map[string]*pb.PlayerStateProto)
	for pid, p := range s.players {
		playersProto[pid] = convertPlayerStateToProto(p)
	}
	serversProto := make(map[string]*pb.GameServerStateProto)
	for sid, gs := range gameServers {
		serversProto[sid] = convertGameServerStateToProto(gs)
	}
	vectorClockCopy := make(map[string]int32)
	for k, v := range s.vectorClock {
		vectorClockCopy[k] = v
	}
	nodeID := s.nodeID
	s.mu.Unlock()

	req := &pb.StateSyncRequest{
		Players:     playersProto,
		Servers:     serversProto,
		VectorClock: vectorClockCopy,
		NodeId:      nodeID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.SyncState(ctx, req)
	if err != nil {
		log.Printf("Error en SyncState con peer %s: %v", peerAddress, err)
	}
}

func (s *MatchmakerServer) periodicSync() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		for _, peer := range s.peers {
			if peer != s.nodeID {
				s.syncWithPeer(peer)
			}
		}
	}
}

func (s *MatchmakerServer) monitorServers() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for _, server := range gameServers {
			if server.Status != "DOWN" && now.Sub(server.LastCheck) > 10*time.Second {
				log.Printf("Servidor %s no responde, marcado como DOWN", server.ID)
				server.Status = "DOWN"

				if server.CurrentMatchID != "" {
					match, ok := activeMatches[server.CurrentMatchID]
					if ok {
						for _, pid := range match.PlayerIDs {
							player, exists := s.players[pid]
							if exists {
								player.State = "IN_QUEUE"
								s.queues[player.GamePreference] = append(s.queues[player.GamePreference], player.PlayerId)
								player.MatchID = ""
								player.ServerAddr = ""
								player.VectorClock[s.nodeID]++
							}
						}
						delete(activeMatches, server.CurrentMatchID)
					}
					server.CurrentMatchID = ""
				}
			}
		}
		s.mu.Unlock()
	}
}

func (s *MatchmakerServer) UpdatePlayersStatus(ctx context.Context, req *pb.UpdatePlayersRequest) (*pb.UpdatePlayersResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	serverID := req.ServerId
	serverKey := fmt.Sprintf("server-%d", serverID)
	server, ok := gameServers[serverKey]
	if !ok {
		return &pb.UpdatePlayersResponse{
			StatusCode: 1,
			Message:    fmt.Sprintf("Servidor %d no encontrado", serverID),
		}, nil
	}

	for _, playerID := range req.PlayersIds {
		player, ok := s.players[playerID]
		if !ok {
			log.Printf("Jugador %s no encontrado para actualizar estado", playerID)
			continue
		}

		if player.State == "IN_MATCH" {
			player.State = "IDLE"
			player.MatchID = ""
			player.ServerAddr = ""
			player.VectorClock[s.nodeID]++
			s.vectorClock[s.nodeID]++

			// Actualizar activeMatches
			if match, exists := activeMatches[server.CurrentMatchID]; exists {
				newPlayerIDs := []string{}
				for _, pid := range match.PlayerIDs {
					if pid != playerID {
						newPlayerIDs = append(newPlayerIDs, pid)
					}
				}
				match.PlayerIDs = newPlayerIDs

				// Si no quedan jugadores, eliminar match y actualizar server
				if len(match.PlayerIDs) == 0 {
					delete(activeMatches, server.CurrentMatchID)
					server.CurrentMatchID = ""
					server.Status = "AVAILABLE"
					log.Printf("Match %s finalizado y eliminado tras actualizar estado de jugadores", match.MatchID)
				}
			}
		}
	}

	return &pb.UpdatePlayersResponse{
		StatusCode: 0,
		Message:    "Estados de jugadores actualizados",
	}, nil
}

func makeAddress(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func loadServersArray() {
	host := "gameservers_container"

	ports := map[string]int{
		"server-1": 50055,
		"server-2": 50056,
		"server-3": 50057,
	}

	for id, port := range ports {
		gameServers[id] = &GameServerState{
			ID:             id,
			Address:        makeAddress(host, port),
			Status:         "AVAILABLE",
			LastCheck:      time.Now(),
			CurrentMatchID: "",
		}
	}
}

func main() {
	loadServersArray()

	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "node-1"
	}
	peersEnv := os.Getenv("PEERS") // comma-separated list, e.g. "node-1,node-2,node-3"
	var peers []string
	if peersEnv != "" {
		for _, p := range splitAndTrim(peersEnv) {
			peers = append(peers, p)
		}
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("No se pudo iniciar el servidor: %v", err)
	}
	grpcServer := grpc.NewServer()

	matchmaker := NewMatchmakerServer(nodeID, peers)
	pb.RegisterMatchmakerServiceServer(grpcServer, matchmaker)

	go matchmaker.matchLoop()
	go matchmaker.periodicSync()
	// go matchmaker.monitorServers()

	log.Println("Servidor Matchmaker escuchando en :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Fallo al servir: %v", err)
	}
}

func splitAndTrim(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
