package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/streadway/amqp"
)

type Torneo struct {
	Id           int64
	Region       string
	Entrenadores []Entrenador
	Estado       string // Activo o finalizado
}

type Entrenador struct {
	Id         string `json:"id"`
	Nombre     string `json:"nombre"`
	Region     string `json:"region"`
	Ranking    int    `json:"ranking"`
	Estado     string `json:"estado"`
	Suspendido int    `json:"suspension"`
}

/* Envía una notificación de confirmación de inscripción a un torneo para un entrenador específico mediante RabbitMQ. */
func enviarConfirmacionDeInscripcion(ch *amqp.Channel, torneo *Torneo, entrenador *Entrenador) {
	mensaje := map[string]interface{}{
		"tipo_mensaje":      "confirmacion_inscripcion",
		"id_entrenador":     entrenador.Id,
		"nombre_entrenador": entrenador.Nombre,
		"torneo_id":         torneo.Id,
		"region":            torneo.Region,
	}

	routingKey := fmt.Sprintf("entrenadores.%s.inscripcion_confirmada", entrenador.Id)
	publicarMensaje(ch, routingKey, mensaje)
}

/* Informa sanciones por abandono, fraude o combate invalido */
func enviarPenalizacion(ch *amqp.Channel, entrenador *Entrenador, motivo string) {
	mensaje := map[string]interface{}{
		"tipo_mensaje":  "penalizacion",
		"id_entrenador": entrenador.Id,
		"motivo":        motivo,
		"fecha":         time.Now().Format("2006-01-02"),
	}

	routingKey := fmt.Sprintf("entrenadores.%s.penalizacion", entrenador.Id)
	publicarMensaje(ch, routingKey, mensaje)
}

/* Informa la disponibilidad de un nuevo torneo a los entrenadores de una región */
func enviarNuevoTorneoDisponible(ch *amqp.Channel, torneo *Torneo) {
	mensaje := map[string]interface{}{
		"tipo_mensaje": "nuevo_torneo",
		"torneo_id":    torneo.Id,
		"region":       torneo.Region,
		"fecha":        time.Now().Format("2006-01-02"),
	}

	routingKey := fmt.Sprintf("entrenadores.nuevo_torneo.%s", torneo.Region)
	publicarMensaje(ch, routingKey, mensaje)
}

/* Informa el ranking actualizado de un entrenador tras un torneo, indicando si es ganador o no */
func enviarRankingActualizado(ch *amqp.Channel, entrenador *Entrenador, torneoId int64, ganador bool, nuevoRanking int) {
	mensaje := map[string]interface{}{
		"tipo_mensaje":      "ranking_actualizado",
		"id_entrenador":     entrenador.Id,
		"nombre_entrenador": entrenador.Nombre,
		"nuevo_ranking":     nuevoRanking,
		"torneo_id":         torneoId,
		"ganador":           ganador,
		"fecha":             time.Now().Format("2006-01-02"),
	}

	routingKey := fmt.Sprintf("entrenadores.%s.ranking_actualizado", entrenador.Id)
	publicarMensaje(ch, routingKey, mensaje)
}

/* Publica un mensaje JSON en el exchange "notificaciones" de RabbitMQ con una routing key específica. */
func publicarMensaje(ch *amqp.Channel, routingKey string, mensaje map[string]interface{}) {
	body, err := json.Marshal(mensaje)
	if err != nil {
		log.Printf("[SNP] Error al serializar mensaje (%s): %v", routingKey, err)
		return
	}

	err = ch.Publish(
		"notificaciones", // exchange
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		log.Printf("[SNP] Error al publicar mensaje (%s): %v", routingKey, err)
		return
	}
}

/* Procesa mensajes entrantes desde RabbitMQ y reenvía al entrenador o grupo correspondiente según su tipo (rechazo, actualizacion ranking, nuevo torneo, etc..) */
func procesarMensajeEntrante(ch *amqp.Channel, body []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(body, &msg); err != nil {
		log.Printf("[SNP] Error al deserializar mensaje: %v", err)
		return
	}

	tipo, ok := msg["tipo_mensaje"].(string)
	if !ok {
		log.Printf("[SNP] Mensaje sin tipo_mensaje: %v", msg)
		return
	}

	switch tipo {
	case "rechazo_lcp":
		entrenador1 := Entrenador{
			Id:     fmt.Sprint(msg["id_entrenador"]),
			Nombre: fmt.Sprint(msg["nombre_entrenador"]),
		}
		log.Printf("[SNP] Publicando notificación: Rechazo de LCP para el entrenador %s (ID: %s)", entrenador1.Nombre, entrenador1.Id)
		motivo := fmt.Sprint(msg["motivo"])
		enviarPenalizacion(ch, &entrenador1, motivo)
	case "rechazo_cdp":
		entrenador1 := Entrenador{
			Id: fmt.Sprint(msg["id_entrenador_1"]),
		}
		entrenador2 := Entrenador{
			Id: fmt.Sprint(msg["id_entrenador_2"]),
		}
		motivo := fmt.Sprint(msg["motivo"])
		log.Printf("[SNP] Publicando notificación: Rechazo de CDP entre (ID: %s) y (ID: %s)", entrenador1.Id, entrenador2.Id)
		enviarPenalizacion(ch, &entrenador1, motivo)
		enviarPenalizacion(ch, &entrenador2, motivo)

	case "inscripcion_confirmada":
		entrenador := Entrenador{
			Id:     fmt.Sprint(msg["id_entrenador"]),
			Nombre: fmt.Sprint(msg["nombre_entrenador"]),
		}
		torneo := Torneo{
			Id:     int64(msg["id_torneo"].(float64)),
			Region: fmt.Sprint(msg["region"]),
		}
		log.Printf("[SNP] Publicando notificación: Inscripción confirmada para el entrenador %s (ID: %s) en el torneo %d", entrenador.Nombre, entrenador.Id, torneo.Id)
		enviarConfirmacionDeInscripcion(ch, &torneo, &entrenador)

	case "nuevo_torneo":
		torneo := Torneo{
			Id:     int64(msg["id_torneo"].(float64)),
			Region: fmt.Sprint(msg["region"]),
		}
		log.Printf("[SNP] Publicando notificación: Nuevo torneo disponible: %d en la región %s", torneo.Id, torneo.Region)
		enviarNuevoTorneoDisponible(ch, &torneo)

	case "ranking_actualizado":
		entrenador := Entrenador{
			Id:     fmt.Sprint(msg["id_entrenador"]),
			Nombre: fmt.Sprint(msg["nombre_entrenador"]),
		}

		torneoId := int64(msg["torneo_id"].(float64))
		ganador, ok := msg["ganador"].(bool)
		if !ok {
			log.Println("Error: 'ganador' no es booleano")
			return
		}

		ranking := int(msg["nuevo_ranking"].(float64))
		log.Printf("[SNP] Publicando notificación: Ranking actualizado para el entrenador %s (ID: %s)", entrenador.Nombre, entrenador.Id)
		enviarRankingActualizado(ch, &entrenador, torneoId, ganador, ranking)

	default:
		log.Printf("[SNP] Tipo de mensaje desconocido: %s", tipo)
	}
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

var (
	rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
)

func main() {
	// Conexión a RabbitMQ
	conn, err := amqp.Dial(rabbitURL)
	failOnError(err, "[SNP] Fallo al conectar con RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "[SNP] Fallo al abrir un canal")
	defer ch.Close()

	// Declara el exchange
	err = ch.ExchangeDeclare(
		"notificaciones", // nombre
		"topic",          // tipo
		true,             // durable
		false,            // auto-deleted
		false,            // internal
		false,            // no-wait
		nil,              // argumentos
	)
	failOnError(err, "[SNP] Fallo al declarar el exchange")

	// Declarar una cola temporal y exclusiva
	q, err := ch.QueueDeclare(
		"",    // nombre generado por el servidor
		false, // durable
		true,  // auto-delete
		true,  // exclusiva
		false, // no-wait
		nil,   // argumentos
	)
	failOnError(err, "[SNP] Fallo al declarar la cola")

	// Vincular la cola al exchange con la routing key 'snp.penalizacion'
	err = ch.QueueBind(
		q.Name,             // nombre de la cola
		"snp.penalizacion", // routing key
		"notificaciones",   // nombre del exchange
		false,
		nil,
	)
	failOnError(err, "[SNP] Fallo al vincular la cola al exchange")

	// Vincula la cola al exchange con routing key actualizaciones de ranking
	err = ch.QueueBind(
		q.Name,                // nombre de la cola
		"snp.actualizaciones", // routing key
		"notificaciones",      // nombre del exchange
		false,
		nil,
	)
	failOnError(err, "[SNP] Fallo al vincular la cola a 'snp.actualizaciones'")

	err = ch.QueueBind(
		q.Name,
		"snp.torneo_disponible",
		"notificaciones",
		false,
		nil,
	)
	failOnError(err, "[SNP] Fallo al vincular la cola a 'snp.torneo_disponible'")

	// Vincular la cola al exchange con la routing key 'snp.inscripcion_confirmada'
	err = ch.QueueBind(
		q.Name,
		"snp.inscripcion_confirmada",
		"notificaciones",
		false,
		nil,
	)
	failOnError(err, "[SNP] Fallo al vincular la cola a 'snp.inscripcion_confirmada'")

	// Consumir mensajes de la cola
	msgs, err := ch.Consume(
		q.Name, // nombre de la cola
		"",     // nombre del consumidor
		true,   // auto-ack
		false,  // exclusivo
		false,  // no-local
		false,  // no-wait
		nil,    // argumentos
	)
	failOnError(err, "[SNP] Fallo al registrar el consumidor")

	log.Println("[SNP] Esperando mensajes de penalización...")

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			log.Println("[SNP] Mensaje recibido en SNP: Procesando y derivando al entrenador o grupo correspondiente.")
			procesarMensajeEntrante(ch, d.Body)
		}
	}()

	<-forever
}
