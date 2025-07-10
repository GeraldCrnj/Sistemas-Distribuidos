package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	pb "main.go/proto/compiled"
)

type Entrenador struct {
	Id         string `json:"id"`
	Nombre     string `json:"nombre"`
	Region     string `json:"region"`
	Ranking    int    `json:"ranking"`
	Estado     string `json:"estado"`
	Suspendido int    `json:"suspension"`
}

type Historial struct {
	Participaciones []Participacion `json:"participaciones"`
}

type Participacion struct {
	TorneoID  int64  `json:"torneo_id"`
	Region    string `json:"region"`
	Resultado string `json:"resultado"`
	Fecha     string `json:"fecha"`
}

var (
	entrenadores        []Entrenador
	regionesDisponibles = []string{"Kanto", "Johto", "Unova", "Sinnoh", "Hoenn"}
	entrenadorConsola   Entrenador
)

const historialFile = "data/historial_consola.json"

/* Carga el historial local */
func cargarHistorial() Historial {
	var historial Historial
	data, err := os.ReadFile(historialFile)
	if err != nil {
		return historial // vacío si no existe
	}
	json.Unmarshal(data, &historial)
	return historial
}

/* Guarda el historial local */
func guardarHistorial(historial Historial) {
	data, _ := json.MarshalIndent(historial, "", "  ")
	err := os.WriteFile(historialFile, data, 0644)
	if err != nil {
		log.Printf("Error al guardar historial: %v", err)
	}
}

/* Simula el comportamiento de un entrenador, consultando torneos y tratando de inscribirse */
func simularEntrenador(ent Entrenador, cliente pb.LCP_EntrenadoresClient) {
	for {
		// Espera aleatoria
		tiempo := time.Duration(rand.Intn(50)+7) * time.Second
		time.Sleep(tiempo)

		torneosResp, err := cliente.ConsultarTorneosDisponibles(context.Background(), &pb.QueryRequest{
			Entrenador: convertirEntrenador(ent),
		})
		if err != nil {
			fmt.Printf("[Entrenador %s] Error al obtener torneos disponibles: %v", ent.Nombre, err)
			continue
		}

		if len(torneosResp.Torneos) == 0 {
			continue
		}

		// Elige un torneo aleatorio entre los torneos disponibles según la región del entrenador simulado
		randomIndex := rand.Intn(len(torneosResp.Torneos))
		torneoSeleccionado := torneosResp.Torneos[randomIndex]
		idTorneoSeleccionado := torneoSeleccionado.Id

		//Solicitud de inscripción
		req := &pb.InscripcionRequest{
			IdTorneo:   idTorneoSeleccionado,
			Entrenador: convertirEntrenador(ent),
		}

		resp, err := cliente.InscribirEntrenador(context.Background(), req)
		if err != nil {
			fmt.Printf("[Entrenador %s] Error al inscribirse: %v", ent.Nombre, err)
			continue
		}

		respuestaInscripcion := obtenerRespuestaInscripcion(resp.Status, ent, idTorneoSeleccionado)
		fmt.Print(respuestaInscripcion)
	}
}

/* Abre el menú principal para el entrenador consola */
func abrirMenu(cliente pb.LCP_EntrenadoresClient) {
	imprimirMenu()
	var opcion string
	for {
		fmt.Scan(&opcion)

		switch opcion {
		case "1":
			fmt.Println("----------------------------------------------------------")
			fmt.Println("[Menú] Consultando torneos disponibles...")
			resp, err := cliente.ConsultarTorneosDisponibles(context.Background(), &pb.QueryRequest{
				Entrenador: convertirEntrenador(entrenadorConsola),
			})
			if err != nil {
				fmt.Printf("Error al consultar torneos: %v\n", err)
				continue
			}

			if len(resp.Torneos) == 0 {
				fmt.Println("[Menú] No hay torneos disponibles")
			} else {
				fmt.Println("[Menú] Torneos disponibles:")
				for _, t := range resp.Torneos {
					fmt.Printf("Torneo: %+v\n", t)
				}
			}
			fmt.Println("----------------------------------------------------------")
		case "2":
			fmt.Println("----------------------------------------------------------")
			var inscripcion int64
			fmt.Println("[Menú] Ingresa el ID del torneo al que deseas inscribirte.")
			fmt.Println("----------------------------------------------------------")

			_, err := fmt.Scan(&inscripcion)
			if err != nil {
				fmt.Println("----------------------------------------------------------")
				fmt.Println("[Menú] Error al leer el ID del torneo")
				imprimirMenu()
				break
			}

			resp, err := cliente.InscribirEntrenador(context.Background(), &pb.InscripcionRequest{
				IdTorneo:   inscripcion,
				Entrenador: convertirEntrenador(entrenadorConsola),
			})
			if err != nil {
				st, ok := status.FromError(err)
				if ok {
					fmt.Println(st.Message())
				} else {
					fmt.Println(err)
				}
				continue
			}

			respuestaInscripcion := obtenerRespuestaInscripcion(resp.Status, entrenadorConsola, inscripcion)
			fmt.Println("----------------------------------------------------------")
			fmt.Print(respuestaInscripcion)
			fmt.Println("----------------------------------------------------------")
		case "3":
			fmt.Println("----------------------------------------------------------")
			fmt.Println("[Entrenador Consola] Últimas notificaciones:")
			if len(ultimasNotificaciones) == 0 {
				fmt.Println("No hay notificaciones recientes.")
			} else {
				for i, n := range ultimasNotificaciones {
					fmt.Printf("%d. %s\n", i+1, n)
				}
			}
			fmt.Println("----------------------------------------------------------")
		case "4":
			fmt.Println("----------------------------------------------------------")
			fmt.Printf("[Entrenador Consola] Tu estado actual es: %s\n", entrenadorConsola.Estado)
			fmt.Println("----------------------------------------------------------")
		case "5":
			fmt.Println("----------------------------------------------------------")
			fmt.Println("[Entrenador Consola] Saliendo del programa.")
			fmt.Println("----------------------------------------------------------")
			return
		case "m":
			imprimirMenu()
		default:
			fmt.Println("----------------------------------------------------------")
			fmt.Println("[Entrenador Consola] Opción no válida.")
			fmt.Println("----------------------------------------------------------")

		}
	}
}

/* Obtiene la respuesta de inscripción según el estado devuelto por el servidor */
func obtenerRespuestaInscripcion(status int32, ent Entrenador, idInscripcion int64) string {
	switch status {
	case 1:
		return fmt.Sprintf("[Entrenador %s] Se ha inscrito al torneo con ID %d\n", ent.Nombre, idInscripcion)
	case 2:
		return fmt.Sprintf("[Entrenador %s] El torneo con ID %d no está disponible\n", ent.Nombre, idInscripcion)
	case 3:
		return fmt.Sprintf("[Entrenador %s] No cumple los requisitos de región para el torneo con ID %d\n", ent.Nombre, idInscripcion)
	case 4:
		return fmt.Sprintf("[Entrenador %s] Ya está inscrito en otro torneo\n", ent.Nombre)
	case 5:
		return fmt.Sprintf("[Entrenador %s] Ya está inscrito en este torneo\n", ent.Nombre)
	case 6:
		return fmt.Sprintf("[Entrenador %s] Existe sobrecupo en el torneo, no puedes inscribirte\n", ent.Nombre)
	case 7:
		ok := actualizarSuspensionEntrenador(ent.Id)
		if ok && ent.Suspendido-1 > 0 {
			return fmt.Sprintf("[Entrenador %s] No puedes inscribirte, estás suspendido por los próximos %d torneos.\n", ent.Nombre, ent.Suspendido-1)
		} else if ok && ent.Suspendido-1 == 0 {
			return fmt.Sprintf("[Entrenador %s] No puedes inscribirte, ahora dejas de estar suspendido.\n", ent.Nombre)
		} else {
			return fmt.Sprintf("[Entrenador %s] Error: no se encontró en lista global de entrenadores para actualizar suspensión.\n", ent.Nombre)
		}
	default:
		return fmt.Sprintf("[Entrenador %s] Respuesta inesperada con status %d\n", ent.Nombre, status)
	}
}

/* Actualiza la suspensión del entrenador, disminuyendo su contador de suspensión */
func actualizarSuspensionEntrenador(idEntrenador string) bool {
	for i := range entrenadores {
		if entrenadores[i].Id == idEntrenador {
			entrenadores[i].Suspendido -= 1
			if entrenadores[i].Suspendido == 0 {
				entrenadores[i].Estado = "Activo"
			}
			return true
		}
	}
	return false // No se encontró el entrenador
}

/* Conecta con el servicio LCP y devuelve la conexión y el cliente */
func conectarConLCP() (*grpc.ClientConn, pb.LCP_EntrenadoresClient) {
	conn, err := grpc.Dial("lcp_container:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("No se pudo conectar a LCP: %v", err)
	}

	cliente := pb.NewLCP_EntrenadoresClient(conn)
	return conn, cliente
}

/* Imprime el menú de opciones para el entrenador consola */
func imprimirMenu() {
	fmt.Println("----------------------------------------------------------")
	fmt.Println("Menú - Opciones:")
	fmt.Println("1. Consultar torneos disponibles.")
	fmt.Println("2. Inscribirse en un torneo seleccionado.")
	fmt.Println("3. Ver notificaciones recibidas.")
	fmt.Println("4. Ver estado actual.")
	fmt.Println("5. Salir del programa.")
	fmt.Println("Escriba 'm' para volver a ver el menú.")
	fmt.Println("----------------------------------------------------------")
}

/* Escoge una región para el entrenador consola */
func escogerRegion() string {
	fmt.Println("Elige la región a la que pertenecerás - Opciones:")
	for i, region := range regionesDisponibles {
		fmt.Printf("%d. %s\n", i+1, region)
	}

	var opcion int
	fmt.Print(">")
	fmt.Scan(&opcion)

	if opcion < 1 || opcion > len(regionesDisponibles) {
		fmt.Println("Opción inválida. Intenta nuevamente")
		return escogerRegion()
	}

	regionEscogida := regionesDisponibles[opcion-1]
	return regionEscogida
}

/* Convierte un objeto Entrenador a su representación en el protocolo gRPC */
func convertirEntrenador(e Entrenador) *pb.Entrenador {
	return &pb.Entrenador{
		Id:         e.Id,
		Nombre:     e.Nombre,
		Region:     e.Region,
		Ranking:    int64(e.Ranking),
		Estado:     e.Estado,
		Suspendido: int64(e.Suspendido),
	}
}

/* Maneja errores y finaliza el programa si hay un error */
func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

/* Escucha las notificaciones de RabbitMQ para el entrenador */
func escucharNotificaciones(ch *amqp.Channel, entrenador Entrenador) {
	q, err := ch.QueueDeclare(
		"",    // nombre aleatorio
		false, // durable
		true,  // auto-delete
		true,  // exclusive
		false, // no-wait
		nil,
	)
	failOnError(err, fmt.Sprintf("[Entrenador %s] Error declarando cola", entrenador.Id))

	routingKey := fmt.Sprintf("entrenadores.%s.*", entrenador.Id)

	err = ch.QueueBind(
		q.Name,
		routingKey,
		"notificaciones",
		false,
		nil,
	)
	failOnError(err, fmt.Sprintf("[Entrenador %s] Error bindeando cola a routing key %s", entrenador.Id, routingKey))

	if entrenador.Nombre == "Consola" {
		routingKey = fmt.Sprintf("entrenadores.nuevo_torneo.%s", entrenador.Region)
		err = ch.QueueBind(
			q.Name,
			routingKey,
			"notificaciones",
			false,
			nil,
		)
		failOnError(err, fmt.Sprintf("[Entrenador %s] Error bindeando cola a routing key %s", entrenador.Id, routingKey))
	}

	msgs, err := ch.Consume(
		q.Name,
		"",    // consumer
		true,  // auto-ack
		true,  // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	failOnError(err, fmt.Sprintf("[Entrenador %s] Error al consumir mensajes", entrenador.Id))

	go func() {
		for d := range msgs {
			procesarMensajeEntrante(d.Body, entrenador)
		}
	}()
}

/* Procesa los mensajes entrantes de RabbitMQ y actúa según el tipo de mensaje */
func procesarMensajeEntrante(body []byte, ent Entrenador) {
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
	case "penalizacion":
		entrenador := Entrenador{
			Nombre: fmt.Sprint(msg["nombre_entrenador"]),
		}

		if entrenador.Nombre == "Consola" {
			motivo := fmt.Sprint(msg["motivo"])
			msgTexto := fmt.Sprintf("Penalización recibida: %s", motivo)
			fmt.Println("------------------------------------------------------")
			fmt.Printf("[¡NOTIFICACION!] %s\n", msgTexto)
			fmt.Println("------------------------------------------------------")
			agregarNotificacion(msgTexto)
		}
	case "confirmacion_inscripcion":
		entrenador := Entrenador{
			Nombre: fmt.Sprint(msg["nombre_entrenador"]),
		}
		id := int64(msg["torneo_id"].(float64))
		region := fmt.Sprint(msg["region"])

		if entrenador.Nombre == "Consola" {
			msgTexto := fmt.Sprintf("Inscripción confirmada al torneo %d en la región %s", id, region)
			fmt.Println("------------------------------------------------------")
			fmt.Printf("[¡NOTIFICACION!] %s\n", msgTexto)
			fmt.Println("------------------------------------------------------")
			agregarNotificacion(msgTexto)
		}
	case "nuevo_torneo":
		id := int64(msg["torneo_id"].(float64))
		region := fmt.Sprint(msg["region"])
		if ent.Nombre == "Consola" {
			msgTexto := fmt.Sprintf("Nuevo torneo disponible: ID %d, Región %s", id, region)
			fmt.Println("------------------------------------------------------")
			fmt.Printf("[¡NOTIFICACION!] %s\n", msgTexto)
			fmt.Println("------------------------------------------------------")
			agregarNotificacion(msgTexto)
		}
	case "ranking_actualizado":
		entrenador := Entrenador{
			Nombre: fmt.Sprint(msg["nombre_entrenador"]),
		}
		ranking := int(msg["nuevo_ranking"].(float64))
		entrenador.Ranking = ranking

		if entrenador.Nombre == "Consola" {
			msgTexto := fmt.Sprintf("Ranking actualizado: Nuevo Ranking %d", entrenador.Ranking)
			fmt.Println("------------------------------------------------------")
			fmt.Printf("[¡NOTIFICACION!] %s\n", msgTexto)
			fmt.Println("------------------------------------------------------")
			agregarNotificacion(msgTexto)

			torneoId := int64(msg["torneo_id"].(float64))
			ganador, ok := msg["ganador"].(bool)
			if !ok {
				log.Println("Error: 'ganador' no es booleano")
				return
			}

			var msgResultado string
			if ganador {
				msgResultado = "Ganador"
			} else {
				msgResultado = "Perdedor"
			}

			historial := cargarHistorial()
			historial.Participaciones = append(historial.Participaciones, Participacion{
				TorneoID:  torneoId,
				Region:    ent.Region,
				Resultado: msgResultado,
				Fecha:     time.Now().Format(time.RFC3339),
			})
			guardarHistorial(historial)
		}
	default:
		fmt.Printf("[Entrenadores] Tipo de mensaje desconocido: %s\n", tipo)
	}
}

/* Agrega las notificaciones del entrenador, útil para el menú */
func agregarNotificacion(mensaje string) {
	if len(ultimasNotificaciones) >= maxNotificaciones {
		ultimasNotificaciones = ultimasNotificaciones[1:]
	}
	ultimasNotificaciones = append(ultimasNotificaciones, mensaje)
}

var (
	ultimasNotificaciones []string
	rabbitURL             = "amqp://guest:guest@rabbitmq:5672/"
)

const (
	maxNotificaciones = 5
)

func main() {
	// Lectura del archivo de entrenadores
	if len(os.Args) < 2 {
		log.Fatalf("Debe especificar el nombre de archivo como argumento")
	}

	archivo := os.Args[1]

	data, err := os.ReadFile(archivo)
	if err != nil {
		log.Fatalf("No se pudo leer el archivo %s: %v", archivo, err)
	}

	// Carga de los entrenadores en memoria
	if err := json.Unmarshal(data, &entrenadores); err != nil {
		log.Fatalf("Error al parsear JSON: %v", err)
	}

	conn, cliente := conectarConLCP()
	defer conn.Close()

	connAMQP, err := amqp.Dial(rabbitURL)
	failOnError(err, "[SNP] Fallo al conectar con RabbitMQ")
	defer conn.Close()

	ch, err := connAMQP.Channel()
	failOnError(err, "[SNP] Fallo al abrir un canal")
	defer ch.Close()

	err = ch.ExchangeDeclare(
		"notificaciones", // nombre
		"topic",          // tipo
		true,             // durable
		false,            // auto-deleted
		false,            // internal
		false,            // no-wait
		nil,              // argumentos
	)
	failOnError(err, "[Entrenadores] Fallo al declarar el exchange")

	for _, e := range entrenadores {
		go simularEntrenador(e, cliente)
		escucharNotificaciones(ch, e)
	}

	entrenadorConsola = Entrenador{
		Id:         strconv.Itoa(len(entrenadores) + 1),
		Nombre:     "Consola",
		Region:     escogerRegion(),
		Ranking:    1500,
		Estado:     "Activo",
		Suspendido: 0,
	}

	escucharNotificaciones(ch, entrenadorConsola)
	abrirMenu(cliente)
}
