package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	pb "main.go/proto/compiled"
)

// printHelp muestra los comandos disponibles al usuario.
func printHelp() {
	fmt.Println("\nComandos disponibles:")
	fmt.Println("  help                          → Muestra esta ayuda")
	fmt.Println("  estado                        → Muestra el estado del sistema")
	fmt.Println("  forzar <id> <estado>          → Fuerza el estado de un servidor (AVAILABLE, DOWN)")
	fmt.Println("  salir                         → Cierra el cliente administrador")
}

// mostrarEstadoSistema realiza la llamada gRPC AdminGetSystemStatus y muestra la información recibida.
func mostrarEstadoSistema(client pb.MatchmakerServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.AdminGetSystemStatus(ctx, &pb.AdminRequest{})
	if err != nil {
		fmt.Printf("Error al obtener estado del sistema: %v\n", err)
		return
	}

	fmt.Println("\n🔧 Servidores de partida:")
	for _, s := range resp.Servers {
		fmt.Printf("  • Server %d - Estado: %-10s Dirección: %s", s.Id, s.Status, s.Address)
		if s.CurrentMatchId != 0 {
			fmt.Printf(" (Match ID: %d)", s.CurrentMatchId)
		}
		fmt.Println()
	}

	fmt.Println("\n⏳ Jugadores en cola:")
	if len(resp.Players) == 0 {
		fmt.Println("  (No hay jugadores en espera)")
	}
	for _, p := range resp.Players {
		duracion := time.Duration(p.TimeInQueue) * time.Second
		fmt.Printf("  • Jugador: %s - Tiempo en cola: %s\n", p.PlayerId, duracion)
	}
}

// forzarEstadoServidor envía la solicitud para cambiar el estado de un servidor mediante AdminUpdateServerState.
func forzarEstadoServidor(client pb.MatchmakerServiceClient, id int32, estado string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &pb.AdminUpdateServerRequest{
		ServerId:        id,
		NewForcedStatus: estado,
	}

	resp, err := client.AdminUpdateServerState(ctx, req)
	if err != nil {
		fmt.Printf("Error al forzar estado del servidor: %v\n", err)
		return
	}

	if resp.StatusCode == 0 {
		fmt.Printf("✅ Estado del servidor %d actualizado a %s correctamente.\n", id, estado)
	} else {
		fmt.Printf("⚠️ No se pudo actualizar el estado del servidor %d: %s\n", id, resp.Message)
	}
}

func main() {
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("No se pudo conectar al Matchmaker: %v", err)
	}
	defer conn.Close()

	client := pb.NewMatchmakerServiceClient(conn)

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🎮 Cliente Administrador conectado al Matchmaker 🎮")
	fmt.Println("Escribe 'help' para ver comandos disponibles")

	for {
		fmt.Print("\n> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		args := strings.Split(input, " ")

		switch strings.ToLower(args[0]) {
		case "help":
			printHelp()

		case "estado":
			mostrarEstadoSistema(client)

		case "forzar":
			if len(args) != 3 {
				fmt.Println("Uso: forzar <server_id> <nuevo_estado>")
				continue
			}
			serverID, err := strconv.Atoi(args[1])
			if err != nil {
				fmt.Println("ID inválido.")
				continue
			}
			nuevoEstado := strings.ToUpper(args[2])
			forzarEstadoServidor(client, int32(serverID), nuevoEstado)

		case "salir", "exit", "quit":
			fmt.Println("Cerrando cliente administrador.")
			return

		default:
			fmt.Println("Comando no reconocido. Escribe 'help' para ver opciones.")
		}
	}
}
