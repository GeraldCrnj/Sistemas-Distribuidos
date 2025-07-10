package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
	pb "main.go/proto/compiled"
)

type gimnasioServer struct {
	pb.UnimplementedLCP_GimnasioServer
	id       int
	region   string
	claveAES []byte
	channel  *amqp.Channel
	exchange string
}

type Entrenador struct {
	Id         string `json:"id"`
	Nombre     string `json:"nombre"`
	Region     string `json:"region"`
	Ranking    int64  `json:"ranking"`
	Estado     string `json:"estado"`
	Suspendido int64  `json:"suspension"`
}

type ResultadoCombate struct {
	TorneoId          int64  `json:"torneoId"`
	IdEntrenador1     string `json:"idEntrenador1"`
	NombreEntrenador1 string `json:"nombreEntrenador1"`
	IdEntrenador2     string `json:"idEntrenador2"`
	NombreEntrenador2 string `json:"nombreEntrenador2"`
	IdGanador         string `json:"idGanador"`
	NombreGanador     string `json:"nombreGanador"`
	Fecha             string `json:"fecha"`
	TipoMensaje       string `json:"tipoMensaje"`
}

/* SimularCombate simula un combate entre dos entrenadores y devuelve el ID del ganador. */
func SimularCombate(ent1, ent2 Entrenador) string {
	diff := float64(ent1.Ranking - ent2.Ranking)
	k := 100.0 // Factor de escala
	prob := 1.0 / (1.0 + math.Exp(-diff/k))
	if rand.Float64() <= prob {
		return ent1.Id
	}
	return ent2.Id
}

/* Convierte un Entrenador protobuf a una estructura Entrenador. */
func convertirEntrenador(e *pb.Entrenador) Entrenador {
	return Entrenador{
		Id:         e.Id,
		Nombre:     e.Nombre,
		Region:     e.Region,
		Ranking:    e.Ranking,
		Estado:     e.Estado,
		Suspendido: e.Suspendido,
	}
}

/* encryptAES cifra el texto plano usando AES-256 en modo GCM. */
func encryptAES(plainText []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	cipherText := gcm.Seal(nonce, nonce, plainText, nil)
	return cipherText, nil
}

/* Implementación del método gRPC AsignarCombate */
func (s *gimnasioServer) AsignarCombate(ctx context.Context, req *pb.CombateRequest) (*pb.CombateResponse, error) {
	log.Printf("[Gimnasio %s] Combate recibido: [%s (%s)] vs [%s (%s)] | Torneo ID: %d",
		s.region, req.Entrenador1.Nombre, req.Entrenador1.Id, req.Entrenador2.Nombre, req.Entrenador2.Id, req.TorneoId)

	// 1. Simular combate (determinar ganador y nuevos ratings)
	Id_Ganador := SimularCombate(convertirEntrenador(req.Entrenador1), convertirEntrenador(req.Entrenador2))
	fechaCombate := time.Now().Format("2006-01-02") // Convierte a YYYY-MM-DD

	var nombreGanador string
	if Id_Ganador == req.Entrenador1.Id {
		nombreGanador = req.Entrenador1.Nombre
	} else {
		nombreGanador = req.Entrenador2.Nombre
	}

	log.Printf("[Gimnasio %s] Resultado del combate: Ganador -> %s (%s) | Fecha: %s",
		s.region, nombreGanador, Id_Ganador, fechaCombate)

	// 2. Construir ResultadoCombate
	res := ResultadoCombate{
		TorneoId:          req.TorneoId,
		IdEntrenador1:     req.Entrenador1.Id,
		NombreEntrenador1: req.Entrenador1.Nombre,
		IdEntrenador2:     req.Entrenador2.Id,
		NombreEntrenador2: req.Entrenador2.Nombre,
		IdGanador:         Id_Ganador,
		NombreGanador:     nombreGanador,
		Fecha:             fechaCombate,
		TipoMensaje:       "resultado_combate",
	}

	// 3. Serializar a JSON
	data, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("[Gimnasio %s] Error al serializar resultado del combate: %v", s.region, err)
	}

	// 4. Cifrar con AES-256
	dataCifrada, err := encryptAES(data, s.claveAES)
	if err != nil {
		return nil, fmt.Errorf("[Gimnasio %s] Error al cifrar resultado del combate: %v", s.region, err)
	}

	// 5. Publicar en RabbitMQ
	routingKey := obtenerRoutingKey(s.region)
	for attempt := 1; attempt <= 3; attempt++ {
		publishErr = s.channel.Publish(
			s.exchange,
			routingKey,
			false,
			false,
			amqp.Publishing{
				ContentType: "application/octet-stream",
				Body:        dataCifrada,
			})

		if publishErr == nil {
			log.Printf("[Gimnasio %s] Resultado publicado en RabbitMQ exitosamente (intento %d).", s.region, attempt)
			break
		}

		log.Printf("[Gimnasio %s] Intento %d fallido al publicar en RabbitMQ: %v", s.region, attempt, publishErr)
		time.Sleep(2 * time.Second) // Pausa entre reintentos
	}

	if publishErr != nil {
		log.Printf("[Gimnasio %s] Error final tras %d intentos, registrando en log_errores.txt.", s.region, 3)
		logToFile(s.region, publishErr.Error())
		return nil, fmt.Errorf("[Gimnasio %s] Error al publicar mensaje tras reintentos: %v", s.region, publishErr)
	}
	// 6. Retornar estado "ok"
	return &pb.CombateResponse{Estado: "ok"}, nil
}

/* logToFile registra errores en un archivo de texto. */
func logToFile(region string, errorMessage string) {
	f, err := os.OpenFile("log_errores.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[Gimnasio %s] Error al abrir log de errores: %v", region, err)
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Printf("[Gimnasio %s] Error al cerrar archivo de log: %v", region, cerr)
		}
	}()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] [Gimnasio %s] %s\n", timestamp, region, errorMessage)

	if _, writeErr := f.WriteString(logEntry); writeErr != nil {
		log.Printf("[Gimnasio %s] Error al escribir en archivo de log: %v", region, writeErr)
	}
}

/* obtenerRoutingKey construye la clave de enrutamiento para RabbitMQ basada en la región. */
func obtenerRoutingKey(region string) string {
	return "resultados." + strings.ToLower(region)
}

/* obtenerAESKey obtiene la clave AES de las variables de entorno. */
func obtenerAESKey(region string) string {
	envKey := "AES_" + strings.ToUpper(region)
	key := os.Getenv(envKey)

	if key == "" {
		log.Fatalf("[Gimnasio %s] AES Key no definida. Esperada en variable de entorno: %s", region, envKey)
	}

	if len(key) != 32 {
		log.Fatalf("[Gimnasio %s] AES Key inválida. Debe tener exactamente 32 caracteres, tiene %d", region, len(key))
	}

	return key
}

/* runGimnasio inicia el servidor gRPC y configura RabbitMQ para un gimnasio específico. */
func runGimnasio(id int, region string, port int) {
	// Conexión con RabbitMQ
	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		log.Fatalf("[Gimnasio %s] Error al conectar a RabbitMQ: %v", region, err)
	}

	channel, err := conn.Channel()
	if err != nil {
		log.Fatalf("[Gimnasio %s] Error al abrir canal RabbitMQ: %v", region, err)
	}

	err = channel.ExchangeDeclare(
		"resultados",
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("[Gimnasio %s] Error al declarar exchange: %v", region, err)
	}

	address := ":" + strconv.Itoa(port)

	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("[Gimnasio %s] Error al escuchar en %s: %v", region, address, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterLCP_GimnasioServer(grpcServer, &gimnasioServer{
		id:       id,
		region:   region,
		claveAES: []byte(obtenerAESKey(region)),
		channel:  channel,
		exchange: "resultados",
	})

	log.Printf("[Gimnasio %s] Escuchando correctamente en %s", region, address)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("[Gimnasio %s] Fallo al servir: %v", region, err)
	}
}

var (
	regiones   = []string{"Kanto", "Johto", "Hoenn", "Sinnoh", "Unova"}
	basePort   = 50052
	rabbitURL  = "amqp://guest:guest@rabbitmq:5672/"
	publishErr error
)

func main() {
	var wg sync.WaitGroup

	for i, region := range regiones {
		port := basePort + i
		wg.Add(1)

		go func(id int, region string, port int) {
			defer wg.Done()
			runGimnasio(id, region, port)
		}(i, region, port)
	}

	wg.Wait()
}
