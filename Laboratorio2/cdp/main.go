package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
	pb "main.go/proto/compiled"
)

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

type RechazoCDP struct {
	TipoMensaje   string `json:"tipo_mensaje"`
	TorneoId      int64  `json:"torneo_id"`
	IdEntrenador1 string `json:"id_entrenador_1"`
	IdEntrenador2 string `json:"id_entrenador_2"`
	Motivo        string `json:"motivo"`
}

/* Desencripta un mensaje AES-256 */
func decryptAES(cipherText []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(cipherText) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext demasiado corto")
	}
	nonce := cipherText[:gcm.NonceSize()]
	cipherData := cipherText[gcm.NonceSize():]
	return gcm.Open(nil, nonce, cipherData, nil)
}

/* Comprueba si un torneo ya fue procesado */
func torneoYaProcesado(id int64) bool {
	for _, t := range torneosProcesados {
		if t == id {
			return true
		}
	}
	return false
}

/* Comprueba si la fecha tiene el formato YYYY-MM-DD, con fechas reales */
func validarFecha(fecha string) bool {
	_, err := time.Parse("2006-01-02", fecha)
	return err == nil
}

/* Valida si los entrenadores están activos usando gRPC */
func validarEntrenadores(cdp_lcp_client pb.CDP_LCPClient, id1, id2 string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req := &pb.ValidarEntrenadoresRequest{
		Id1: id1,
		Id2: id2,
	}
	resp, err := cdp_lcp_client.ValidarEntrenadores(ctx, req)
	if err != nil {
		return false, err
	}
	// Suponemos que ValidarEntrenadoresResponse tiene un campo "Activos" bool
	return resp.Activos, nil
}

/* Notifica al SNP sobre un rechazo de CDP */
func notificarSNP(ch *amqp.Channel, res ResultadoCombate, motivo string) {
	payload, err := json.Marshal(RechazoCDP{
		TipoMensaje:   "rechazo_cdp",
		TorneoId:      res.TorneoId,
		IdEntrenador1: res.IdEntrenador1,
		IdEntrenador2: res.IdEntrenador2,
		Motivo:        motivo,
	})
	if err != nil {
		log.Printf("[CDP] Error serializando JSON de rechazo: %v", err)
		return
	}

	err = ch.Publish(
		"notificaciones",   // exchange SNP
		"snp.penalizacion", // routing key
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        payload,
		},
	)
	if err != nil {
		log.Printf("[CDP] Error notificando al SNP: %v", err)
	} else {
		log.Printf("[CDP] Notificación al SNP enviada: %s", payload)
	}
}

/* Procesa un mensaje recibido de RabbitMQ. Notifica al SNP si hay errores */
func procesaMensajeCDP(plaintext []byte, cdp_lcp_client pb.CDP_LCPClient, ch *amqp.Channel) {
	// Parseo JSON
	var res ResultadoCombate
	if err := json.Unmarshal(plaintext, &res); err != nil {
		log.Printf("[CDP] JSON inválido: %v — raw: %s", err, string(plaintext))
		return
	}

	// Validación de campos no vacíos
	if res.IdEntrenador1 == "" ||
		res.IdEntrenador2 == "" ||
		res.IdGanador == "" ||
		res.Fecha == "" ||
		res.TipoMensaje == "" {
		log.Printf("[CDP] Faltan campos requeridos en el mensaje: %+v", res)
		return
	}

	// Validar que TipoMensaje sea el esperado
	if res.TipoMensaje != "resultado_combate" {
		log.Printf("[CDP] TipoMensaje incorrecto: %s", res.TipoMensaje)
		return
	}

	// Validar formato de fecha
	if !validarFecha(res.Fecha) {
		log.Printf("[CDP] Fecha con formato inválido: %s", res.Fecha)
		return
	}

	activos, err := validarEntrenadores(cdp_lcp_client, res.IdEntrenador1, res.IdEntrenador2)
	if err != nil {
		log.Printf("[CDP] Error al validar entrenadores: %v", err)
		notificarSNP(ch, res, "error_validacion_grpc")
		return
	}
	if !activos {
		log.Printf("[CDP] Entrenadores no válidos o no activos: %s, %s", res.IdEntrenador1, res.IdEntrenador2)
		notificarSNP(ch, res, "entrenadores_inactivos")
		return
	}
	log.Printf("[CDP] Entrenadores válidos y activos en torneo %d: %s, %s", res.TorneoId, res.IdEntrenador1, res.IdEntrenador2)

	// Verificar que el ganador esté entre los dos entrenadores
	if res.IdGanador != res.IdEntrenador1 && res.IdGanador != res.IdEntrenador2 {
		log.Printf("[CDP] IdGanador no coincide con ningún participante: %s", res.IdGanador)
		return
	}

	// Si llegas hasta aquí, la estructura es válida
	//log.Printf("[CDP] Mensaje estructuralmente válido: %+v", res)

	if torneoYaProcesado(res.TorneoId) {
		log.Printf("[CDP] Torneo %d ya procesado, se omite este combate.", res.TorneoId)
		return
	}

	torneosProcesados = append(torneosProcesados, res.TorneoId)
	log.Printf("[CDP] Procesando por primera vez torneo con ID %d.", res.TorneoId)

	var pubErr error
	for intento := 1; intento <= 3; intento++ {
		pubErr = ch.Publish(
			"resultados_validos", // exchange destino
			"lcp.resultado",      // routing key destino (el que LCP escucha)
			false, false,
			amqp.Publishing{
				ContentType: "application/json",
				Body:        plaintext,
			},
		)
		if pubErr == nil {
			log.Printf("[CDP] Mensaje enviado a LCP (intento %d): %s en torneo %d", intento, res.TipoMensaje, res.TorneoId)
			break
		}
		log.Printf("[CDP] Error publicando a LCP (intento %d): %v", intento, pubErr)
		time.Sleep(500 * time.Millisecond) // breve espera antes de reintentar
	}

	// Si tras 3 intentos sigue fallando, lo notificamos como fallo crítico
	if pubErr != nil {
		log.Println("[CDP] No se pudo notificar a LCP tras 3 intentos, registro como fallo.")
		notificarSNP(ch, res, "error_publish_lcp")
	}
}

/* Maneja errores de forma centralizada */
func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

/* Obtiene la clave AES de las variables de entorno según la región */
func obtenerAESKey(region string) string {
	envKey := "AES_" + strings.ToUpper(region)
	key := os.Getenv(envKey)

	if key == "" {
		log.Fatalf("[Gimnasio %s] AES Key no definida. Esperada en variable de entorno: %s", region, envKey)
	}

	if len(key) != 32 {
		log.Fatalf("[Gimnasio %s] AES Key inválida. Debe tener exactamente 32 caracteres, tiene %d.", region, len(key))
	}

	return key
}

/* Capitaliza la primera letra de una cadena y pone el resto en minúsculas */
func capitalizar(s string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

var (
	rabbitURL         = "amqp://guest:guest@rabbitmq:5672/"
	torneosProcesados []int64
)

func main() {
	conn, err := amqp.Dial(rabbitURL)
	failOnError(err, "[CDP] Fallo al conectar con RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "[CDP] Error al abrir el canal")
	defer ch.Close()

	err = ch.ExchangeDeclare(
		"resultados", // nombre exchange
		"topic",      // tipo exchange
		true,         // durable
		false,        // auto-delete
		false,        // internal
		false,        // no-wait
		nil,          // args
	)
	failOnError(err, "[CDP] Fallo al declarar el exchange 'resultados'")

	// Exchange para enviar resultados válidos a la LCP
	err = ch.ExchangeDeclare(
		"resultados_validos", // nombre del exchange destino
		"topic",              // tipo (puede ser “fanout” o “topic” según tu diseño)
		true, false, false, false, nil,
	)
	failOnError(err, "[CDP] Fallo al declarar el exchange 'resultados_validos'")

	// Exchange para el SNP
	err = ch.ExchangeDeclare(
		"notificaciones",
		"topic",
		true, false, false, false, nil,
	)
	failOnError(err, "[CDP] Fallo al declarar el exchange 'notificaciones'")

	queue, err := ch.QueueDeclare(
		"",
		false, // durable
		true,  // delete cuando se cierre conexión
		true,  // exclusiva
		false,
		nil,
	)
	failOnError(err, "[CDP] Fallo al declarar la cola de mensajes")

	grpcConn, err := grpc.Dial("lcp:50051", grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(5*time.Second))
	failOnError(err, "[CDP] No se pudo conectar a LCP vía gRPC")
	defer grpcConn.Close()
	cdp_lcp_client := pb.NewCDP_LCPClient(grpcConn)

	regiones := []string{"Kanto", "Johto", "Hoenn", "Sinnoh", "Unova"}

	for _, region := range regiones {
		routingKey := "resultados." + strings.ToLower(region)
		err = ch.QueueBind(
			queue.Name,   // cola
			routingKey,   // routing key patrón
			"resultados", // exchange
			false,
			nil,
		)
		failOnError(err, "[CDP] Fallo al hacer el bind de la cola de mensajes (queue bind)")
	}

	msgs, err := ch.Consume(
		queue.Name,
		"",
		true,  // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	failOnError(err, "[CDP] Fallo al registrar el consumidor CDP.")

	log.Println("[CDP] Esperando mensajes de las regiones:", regiones)

	for msg := range msgs {
		log.Printf("[CDP] Mensaje cifrado recibido con routing key '%s'", msg.RoutingKey)

		// Extraer la región de la routing key para obtener la clave AES correcta
		routingParts := strings.Split(msg.RoutingKey, ".")
		if len(routingParts) != 2 {
			log.Printf("[CDP] Routing key inválida: %s", msg.RoutingKey)
			continue
		}
		region := routingParts[1]

		// Obtener AES Key según región (implementa esta función según tu lógica)
		key := obtenerAESKey(region)

		// Desencriptar mensaje
		plaintext, err := decryptAES(msg.Body, []byte(key))
		if err != nil {
			log.Printf("[CDP] Error desencriptando mensaje de %s: %v", capitalizar(region), err)
			continue
		}

		// Procesar mensaje desencriptado
		procesaMensajeCDP(plaintext, cdp_lcp_client, ch)

		log.Printf("[CDP] Mensaje desde %s desencriptado correctamente", capitalizar(region))
	}
}
