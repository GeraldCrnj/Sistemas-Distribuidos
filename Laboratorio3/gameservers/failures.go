package main

import (
	"log"
	"math/rand"
	"time"
)

func startFailureSimulation(gs *GameServer) {
	go func() {
		for {
			// Espera aleatoria antes de simular caída
			sleepTime := time.Duration(60+rand.Intn(120)) * time.Second
			time.Sleep(sleepTime)

			log.Printf("[%s] simulando caída", gs.ID)
			if err := gs.updateMatchmakerStatus("DOWN"); err != nil {
				log.Printf("Error actualizando estado a DOWN: %v", err)
				continue
			}

			// Tiempo aleatorio de recuperación
			recoverTime := time.Duration(20+rand.Intn(30)) * time.Second
			time.Sleep(recoverTime)

			gs.mu.Lock()
			currentStatus := gs.Status
			gs.mu.Unlock()

			if currentStatus == "DOWN" {
				log.Printf("[%s] volviendo a estado AVAILABLE", gs.ID)
				if err := gs.updateMatchmakerStatus("AVAILABLE"); err != nil {
					log.Printf("Error actualizando estado a AVAILABLE: %v", err)
				}
			}
		}
	}()
}
