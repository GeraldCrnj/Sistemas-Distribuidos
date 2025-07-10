package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"google.golang.org/grpc"
	pb "main.go/proto/compiled"
)

type VectorClock map[string]int32

type Player struct {
	Id          string
	State       string // "IDLE", "IN_QUEUE", "IN_MATCH"
	VectorClock VectorClock
	MatchID     string
	ServerAddr  string
	sync.Mutex
}

func NewPlayer(id string) *Player {
	return &Player{
		Id:          id,
		State:       "IDLE",
		VectorClock: make(VectorClock),
	}
}

func (p *Player) incrementClock() {
	p.VectorClock[p.Id]++
}

func (p *Player) mergeClock(vc map[string]int32) {
	for k, v := range vc {
		if current, ok := p.VectorClock[k]; !ok || v > current {
			p.VectorClock[k] = v
		}
	}
}

func (p *Player) queue(ctx context.Context, client pb.MatchmakerServiceClient) {
	p.incrementClock()
	req := &pb.PlayerInfoRequest{
		PlayerId:           p.Id,
		GameModePreference: "1v1",
		VectorClock:        p.VectorClock,
	}
	resp, err := client.QueuePlayer(ctx, req)
	if err != nil {
		log.Printf("[%s] Error al unirse a la cola: %v", p.Id, err)
		return
	}
	p.mergeClock(resp.VectorClock)
	fmt.Printf("[%s] %s\n", p.Id, resp.Message)
	if resp.StatusCode == 0 {
		p.State = "IN_QUEUE"
	}
}

func (p *Player) getStatus(ctx context.Context, client pb.MatchmakerServiceClient) {
	req := &pb.PlayerStatusRequest{
		PlayerId: p.Id,
	}
	resp, err := client.GetPlayerStatus(ctx, req)
	if err != nil {
		log.Printf("[%s] Error al obtener estado: %v", p.Id, err)
		return
	}
	p.State = resp.Status
	p.MatchID = resp.MatchId
	p.ServerAddr = resp.ServerAddress
	p.mergeClock(resp.VectorClock)
}

func (p *Player) cancelQueue(ctx context.Context, client pb.MatchmakerServiceClient) {
	p.incrementClock()
	req := &pb.CancelRequest{
		PlayerId:    p.Id,
		VectorClock: p.VectorClock,
	}
	resp, err := client.CancelMatchmaking(ctx, req)
	if err != nil {
		log.Printf("[%s] Error al cancelar emparejamiento: %v", p.Id, err)
		return
	}
	p.mergeClock(resp.VectorClock)
	if resp.StatusCode == 0 {
		p.State = "IDLE"
		fmt.Printf("[%s] Emparejamiento cancelado exitosamente.\n", p.Id)
	} else {
		fmt.Printf("[%s] No se pudo cancelar: %s\n", p.Id, resp.Message)
	}
}

func (p *Player) runConsole(client pb.MatchmakerServiceClient) {
	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()
	for {
		fmt.Println("---------------------------")
		fmt.Println("1: Unirse a la cola")
		fmt.Println("2: Consultar estado")
		fmt.Println("3: Cancelar emparejamiento")
		fmt.Println("4: Salir")
		fmt.Println("---------------------------")
		fmt.Print("Seleccione: ")
		scanner.Scan()
		opt := scanner.Text()
		switch opt {
		case "1":
			p.queue(ctx, client)
		case "2":
			p.getStatus(ctx, client)
			fmt.Printf("Tu estado es %s\n", p.State)
		case "3":
			p.cancelQueue(ctx, client)
		case "4":
			fmt.Println("Saliendo...")
			return
		default:
			fmt.Println("Opción inválida")
		}
	}
}

func (p *Player) watchMatchStatus(ctx context.Context, client pb.MatchmakerServiceClient) {
	go func() {
		var lastMatchID string
		for {
			time.Sleep(2 * time.Second)
			p.getStatus(ctx, client)

			if p.State == "IN_MATCH" && p.MatchID != lastMatchID {
				lastMatchID = p.MatchID

				fmt.Printf(">> [%s] ¡Asignado a partida! Conectando a %s (MatchID: %s)\n",
					p.Id, p.ServerAddr, p.MatchID)
				time.Sleep(5 * time.Second)
				fmt.Printf(">> [%s] Partida finalizada. Volviendo a estado IDLE.\n", p.Id)
				p.State = "IDLE"
				p.MatchID = ""
				p.ServerAddr = ""
			}
		}
	}()
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Uso: go run main.go <PlayerID>")
	}
	playerID := os.Args[1]

	conn, err := grpc.Dial("matchmaker_container:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("No se pudo conectar al Matchmaker: %v", err)
	}
	defer conn.Close()

	client := pb.NewMatchmakerServiceClient(conn)
	player := NewPlayer(playerID)

	// Manejo de señales
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		fmt.Println("\nInterrumpido por el usuario.")
		os.Exit(0)
	}()

	player.watchMatchStatus(context.Background(), client)
	fmt.Printf("¡Bienvenido %s!\n", player.Id)
	player.runConsole(client)
}
