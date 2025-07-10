package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	pb "bountyhunters/proto/grpc-server/proto"

	"google.golang.org/grpc"
)

/*Vende un pirata al submundo. Si es fraude, no se paga. Si es válido, se suma el pago a la wallet. */
func SellPirateToUnderworld(pirate *pb.Pirate, client pb.BountyhuntersUnderworldServiceClient) {
	resp, err := client.SellPirate(context.Background(), &pb.SellUnderworldRequest{Pirate: pirate})
	if err != nil {
		log.Printf("Cazarrecompensas_%d: Ocurrió un problema al intentar vender el pirata al submundo.\n", bountyHunterId)
	}

	if resp.IsFraud {
		fmt.Printf("Cazarrecompensas_%d: FRAUDE! El submundo no te paga por el pirata %s.\n", bountyHunterId, pirate.Name)
		return
	}

	wallet += resp.Payment
	soldPiratesUnderworld += 1
	fmt.Printf("Cazarrecompensas_%d: Has ganado +$%d BERRIES entregando al pirata %s al SUBMUNDO.\n", bountyHunterId, resp.Payment, pirate.Name)
}

/* Vende un pirata a la marina. La respuesta depende del estado de reputación y actividad del submundo. */
func SellPirateToMarine(pirate *pb.Pirate, client pb.BountyhuntersMarineServiceClient) int32 {
	resp, err := client.SellPirate(context.Background(), &pb.SellMarineRequest{BountyhunterId: bountyHunterId, Pirate: pirate})
	if err != nil {
		log.Printf("Cazarrecompensas_%d: Ocurrió un problema al intentar vender a la marina: %v.\n", bountyHunterId, err)
	}

	switch resp.StatusCode {
	case 0: // 0: Éxito
		soldPiratesMarine += 1
		fmt.Printf("Cazarrecompensas_%d: La marina te paga $%d por el pirata %s.\n", bountyHunterId, resp.Payment, pirate.Name)
	case 1: // 1: Pago reducido por sobreactividad
		soldPiratesMarine += 1
		fmt.Printf("Cazarrecompensas_%d: La marina decidió pagarte $%d por el pirata %s debido a la actividad alta del submundo.\n", bountyHunterId, resp.Payment, pirate.Name)
	case 2: // 2: Rechazo por reputación
		fmt.Printf("Cazarrecompensas_%d: La marina te rechaza la entrega del pirata %s debido a tu baja reputación.\n", bountyHunterId, pirate.Name)
	case 3: // 3: Rechazo por pirata ya entregado
		fmt.Printf("Cazarrecompensas_%d: La marina te rechaza la entrega del pirata %s debido a que el pirata ya está capturado.\n", bountyHunterId, pirate.Name)
	}

	wallet += int64(resp.Payment)
	return resp.StatusCode
}

/* Determina si el pirata logra escapar según su nivel de peligro. */
func tryEscape(pirateToCatch *pb.Pirate) bool {
	var probabilityToEscape float64

	switch pirateToCatch.DangerLevel {
	case "Alto":
		probabilityToEscape = 0.45
	case "Medio":
		probabilityToEscape = 0.25
	case "Bajo":
		probabilityToEscape = 0.15
	default:
		probabilityToEscape = 0.0
	}

	// Generar número aleatorio entre 0.0 y 1.0
	if rand.Float64() < probabilityToEscape {
		return true // El pirata se escapa
	}

	return false
}

/* Consulta al submundo si enviarán mercenarios a recuperar al pirata. */
func checkIfUnderworldAttacks(pirateToCatch *pb.Pirate, client pb.BountyhuntersUnderworldServiceClient) bool {
	resp, err := client.IsUnderworldSendingMercenaries(context.Background(), &pb.PirateRequest{PirateCatched: pirateToCatch})
	if err != nil {
		fmt.Printf("Cazarrecompensas_%d: Error al consultar al submundo si vienen mercenarios: %v.\n", bountyHunterId, err)
	}

	return resp.IsSendingMercenaries
}

/* Consulta si la marina confiscará al pirata capturado. */
func checkIfMarineIsConfiscating(pirateToCatch *pb.Pirate, client pb.BountyhuntersMarineServiceClient) bool {
	resp, err := client.IsMarineConfiscatingPirate(context.Background(), &pb.MarineConfiscatingRequest{
		PirateCatched:  pirateToCatch,
		BountyHunterId: bountyHunterId,
	})
	if err != nil {
		fmt.Printf("Cazarrecompensas_%d: Error al consultar a la marina si les confiscará el pirata: %v.\n", bountyHunterId, err)
	}

	return resp.IsMarineConfiscating
}

/* Envía un reporte de captura del pirata al gobierno mundial y actualiza la reputación. */
func sendCaptureReport(pirateToCatch *pb.Pirate, state string, buyer string, client pb.BountyhuntersgovernmentClient) {
	resp, err := client.SendCaptureReport(context.Background(), &pb.SendCaptureReportRequest{
		BountyhunterId: bountyHunterId,
		Pirate:         pirateToCatch,
		State:          state,
		Buyer:          buyer,
	})
	if err != nil {
		log.Fatalf("Cazarrecompensas_%d: Error al enviar el informe de captura al gobierno mundial: %v.", bountyHunterId, err)
	}
	reputation = resp.NewReputation

	fmt.Printf("Cazarrecompensas_%d: Se ha enviado el informe de captura del pirata %s al gobierno mundial.\n", bountyHunterId, pirateToCatch.Name)
}

/* Función que usa threads para ir revisando la lista de piratas buscados cada una frecuencia dada */
func updateWantedPirates(client pb.BountyhuntersgovernmentClient) {
	ticker := time.NewTicker(frequencyGetList)
	defer ticker.Stop()

	for {
		<-ticker.C

		resp, err := client.GetWantedPirates(context.Background(), &pb.GetWantedPiratesRequest{})
		if err != nil {
			fmt.Printf("Cazarrecompensas_%d: Error al obtener piratas: %v.\n", bountyHunterId, err)
			continue
		}

		mutex.Lock()
		wantedPirates = resp.Pirates
		mutex.Unlock()

		select {
		case <-updateSignal: // Si ya se ha recibido la señal de la primera actualización, no hacer nada
		default:
			close(updateSignal) // Cerrar el canal para indicar que ya se actualizó por primera vez
		}
	}

}

/* Decide entre vender al submundo o la marina. */
func chooseBestBuyer(pirateToCatch *pb.Pirate) string {
	reward := float64(pirateToCatch.Reward)

	var escapeRisk float64
	var underworldAttackRisk float64
	var marineRaidRisk float64 = 0.0

	switch pirateToCatch.DangerLevel {
	case "Alto":
		escapeRisk = 0.45
		marineRaidRisk = 0.25
		underworldAttackRisk = 0.35 // Solo se aplica en este caso
	case "Medio":
		escapeRisk = 0.25
		underworldAttackRisk = 0.0
	case "Bajo":
		escapeRisk = 0.15
		underworldAttackRisk = 0.0
	}
	// Riesgo de fraude del submundo (constante)
	fraudUnderworldRisk := 0.35

	// Probabilidad de éxito de no perder el pirata antes de entregarlo
	successUnderworld := (1 - escapeRisk) * (1 - underworldAttackRisk) * (1 - fraudUnderworldRisk)

	if successUnderworld > 1.0 {
		successUnderworld = 1.0
	}
	avgUnderworldPay := reward * 1.25
	utilityUnderworld := successUnderworld * avgUnderworldPay

	// Si se ha vendido mucho al submundo, la marina reduce el pago al 50%
	// Se calcula un ratio de si más de 40% de las entregas fueron al submundo se considera como mucha actividad ilegal
	marinePenalty := 1.0
	totalSold := soldPiratesMarine + soldPiratesUnderworld
	if totalSold > 0 {
		ratio := float64(soldPiratesUnderworld) / float64(totalSold)
		if ratio > 0.4 && rand.Float64() < 0.5 {
			marinePenalty = 0.5
		}
	}

	// Probabilidad de éxito con la marina
	successMarine := (1 - escapeRisk) * (1 - marineRaidRisk) * (1 - underworldAttackRisk)
	utilityMarine := successMarine * reward * marinePenalty

	if utilityMarine > utilityUnderworld {
		return "Marina"
	}

	return "Submundo"
}

/* Modifica el estado del pirata en la lista de piratas*/
func modifyPirateState(id int32, newState string) error {
	for _, pirate := range wantedPirates {
		if pirate.Id == id {
			pirate.State = newState
			return nil
		}
	}
	return fmt.Errorf("Pirata con ID %d no encontrado", id)
}

var (
	wantedPirates         []*pb.Pirate
	mutex                 sync.Mutex
	updateSignal                = make(chan struct{}) // Canal para notificar cuando se actualicen los piratas
	reputation            int32 = 50                  // Reputación del cazarrecompensas (0-100)
	soldPiratesUnderworld int32 = 0
	soldPiratesMarine     int32 = 0
	wallet                int64 = 0
	bountyHunterId        int32
)

const (
	timeOperations         = 60 * 6 * time.Second
	timeToCatchPirate      = 2 * time.Second
	timeTransportingPirate = 5 * time.Second
	frequencyGetList       = 20 * time.Second // Tiempo en que se vuelve a consultar la lista de piratas
)

func main() {
	time.Sleep(5 * time.Second)

	// Semilla aleatoria
	rand.Seed(time.Now().UnixNano())

	// Conectar a gRPC server
	connGovernment, err := grpc.Dial("government_container:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Error al conectar al servidor %v", err)
	}
	defer connGovernment.Close()

	connUnderworld, err := grpc.Dial("underworld_container:50052", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Error al conectar al servidor %v", err)
	}
	defer connUnderworld.Close()

	connMarine, err := grpc.Dial("marine_container:50053", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Error al conectar al servidor %v", err)
	}
	defer connMarine.Close()

	// Crea un thread (hilo) que cierra el programa luego de pasar el tiempo de operaciones
	go func() {
		time.Sleep(timeOperations)
		fmt.Println("Tiempo expirado: Cerrando conexiones...")

		connGovernment.Close()
		connUnderworld.Close()
		connMarine.Close()

		fmt.Println("Conexiones cerradas. Finalizando programa.")
		os.Exit(0)
	}()

	clientGovernment := pb.NewBountyhuntersgovernmentClient(connGovernment)
	clientUnderworld := pb.NewBountyhuntersUnderworldServiceClient(connUnderworld)
	clientMarine := pb.NewBountyhuntersMarineServiceClient(connMarine)

	resp, err := clientGovernment.AssignBountyhunterId(context.Background(), &pb.AssignBountyhunterIdRequest{})
	if err != nil {
		log.Fatalf("Error al solicitar BountyhunterId: %v", err)
	}
	bountyHunterId = resp.BountyhunterId

	// Se actualiza la lista de piratas con threads
	go updateWantedPirates(clientGovernment)

	<-updateSignal // Espera a que reciba la primera consulta de la lista de piratas

	for {
		if len(wantedPirates) == 0 {
			fmt.Printf("Cazarrecompensas_%d: No hay más piratas en la lista de recompensas.\n", bountyHunterId)
			return
		}

		// Pirata que se va a intentar capturar
		pirateToCatch := wantedPirates[rand.Intn(len(wantedPirates))]
		fmt.Printf("Cazarrecompensas_%d: Intentaremos atrapar al pirata %s, con recompensa $%d\n", bountyHunterId, pirateToCatch.Name, pirateToCatch.Reward)

		time.Sleep(timeToCatchPirate)

		// Captura exitosa, ahora se debe definir a quien se va a entregar (marina o submundo)
		// Evaluando riesgos y beneficios
		entityToSell := chooseBestBuyer(pirateToCatch)
		if entityToSell == "Marina" {
			fmt.Printf("Cazarrecompensas_%d: Capturamos al pirata %s y lo entregaremos a la MARINA.\n", bountyHunterId, pirateToCatch.Name)
		} else {
			fmt.Printf("Cazarrecompensas_%d: Capturamos al pirata %s y lo entregaremos al SUBMUNDO.\n", bountyHunterId, pirateToCatch.Name)
		}

		// Ahora comienza el proceso de transporte
		// Revisar intentos de fuga
		if tryEscape(pirateToCatch) {
			fmt.Printf("Cazarrecompensas_%d: El pirata %s se ha escapado durante el transporte!\n", bountyHunterId, pirateToCatch.Name)

			sendCaptureReport(pirateToCatch, "Perdido", "", clientGovernment)
			continue
		}

		// Se revisa si el submundo envia mercenarios con un 35% de probabilidad
		isUnderworldAttacking := checkIfUnderworldAttacks(pirateToCatch, clientUnderworld)
		if isUnderworldAttacking {
			fmt.Printf("Cazarrecompensas_%d: Perdiste al pirata %s, fuiste atacado por mercenarios del submundo!\n", bountyHunterId, pirateToCatch.Name)

			sendCaptureReport(pirateToCatch, "Perdido", "", clientGovernment)
			fmt.Printf("Cazarrecompensas_%d: Reputación: %d. Balance: $%d\n", bountyHunterId, reputation, wallet)
			continue
		}

		// Revisa que la marina esté confiscando o no
		isMarineConfiscating := checkIfMarineIsConfiscating(pirateToCatch, clientMarine)
		if isMarineConfiscating {
			fmt.Printf("Cazarrecompensas_%d: La marina te confiscó al pirata %s!\n", bountyHunterId, pirateToCatch.Name)
			fmt.Printf("Cazarrecompensas_%d: Reputación: %d. Balance: $%d\n", bountyHunterId, reputation, wallet)
			modifyPirateState(pirateToCatch.Id, "Capturado") // Si se confisca se marca como capturado en la lista de buscados LOCAL
			continue
		}

		// Tiempo de transporte simulado
		time.Sleep(timeTransportingPirate)

		// Comienza el proceso de entrega al submundo o la marina
		if entityToSell == "Marina" {
			// Se intenta vender a la marina
			statusCode := SellPirateToMarine(pirateToCatch, clientMarine)
			if statusCode == 0 || statusCode == 1 { // 0: Exito, 1: Pago reducido, 2: Rechazado por reputación, 3: Rechazo por pirata ya entregado
				// Se cambia LOCALMENTE el pirata a capturado, para no buscar por él nuevamente
				modifyPirateState(pirateToCatch.Id, "Capturado")
				sendCaptureReport(pirateToCatch, "Entregado", "Marina", clientGovernment)
			}
		} else {
			SellPirateToUnderworld(pirateToCatch, clientUnderworld)
			// Se cambia LOCALMENTE el pirata a capturado, para no buscar por él nuevamente
			modifyPirateState(pirateToCatch.Id, "Capturado")
			sendCaptureReport(pirateToCatch, "Entregado", "Submundo", clientGovernment)
		}
		fmt.Printf("Cazarrecompensas_%d: Reputación: %d. Balance: $%d\n", bountyHunterId, reputation, wallet)
	}
}
