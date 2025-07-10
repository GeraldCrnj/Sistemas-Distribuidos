package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
	pb "main.go/proto/compiled"
)

type server struct {
	pb.UnimplementedLCP_EntrenadoresServer
	clienteGimnasios pb.LCP_GimnasioClient
	mu               sync.Mutex
	chSNP            *amqp.Channel
}

type serverLCP struct {
	pb.UnimplementedCDP_LCPServer
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

type MensajeRanking struct {
	TipoMensaje      string `json:"tipo_mensaje"`
	IdEntrenador     string `json:"id_entrenador"`
	NombreEntrenador string `json:"nombre_entrenador"`
	NuevoRanking     int    `json:"nuevo_ranking"`
	TorneoId         int64  `json:"torneo_id"`
	Ganador          bool   `json:"ganador"`
	Fecha            string `json:"fecha"`
}

type MensajeRechazo struct {
	TipoMensaje      string `json:"tipo_mensaje"`
	IdEntrenador     string `json:"id_entrenador"`
	NombreEntrenador string `json:"nombre_entrenador"`
	Motivo           string `json:"motivo"`
	Fecha            string `json:"fecha"`
}

type MensajeNuevoTorneo struct {
	TipoMensaje string `json:"tipo_mensaje"`
	IdTorneo    int64  `json:"id_torneo"`
	Region      string `json:"region"`
}

type MensajeInscripcionConfirmada struct {
	TipoMensaje      string `json:"tipo_mensaje"`
	IdTorneo         int64  `json:"id_torneo"`
	IdEntrenador     string `json:"id_entrenador"`
	NombreEntrenador string `json:"nombre_entrenador"`
	Region           string `json:"region"`
	Fecha            string `json:"fecha"`
}

/* Muestra los torneos disponibles para el entrenador que consulta */
func (s *server) ConsultarTorneosDisponibles(ctx context.Context, req *pb.QueryRequest) (*pb.QueryResponse, error) {
	var pbTorneosDisponibles []*pb.Torneo
	for _, t := range torneos {
		if t.Estado == "Disponible" && req.Entrenador.Region == t.Region {
			pbTorneosDisponibles = append(pbTorneosDisponibles, convertirTorneo(t))
		}
	}

	return &pb.QueryResponse{
		Torneos: pbTorneosDisponibles,
	}, nil
}

/* Inscribe al entrenador en un torneo, validando que cumpla con las reglas */
func (s *server) InscribirEntrenador(ctx context.Context, req *pb.InscripcionRequest) (*pb.InscripcionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := req.Entrenador
	// Agrega al entrenador a la memoria
	var ent *Entrenador
	if _, existe := entrenadores[e.Id]; !existe {
		tmp := convertirPbEntrenador(e)
		ent = &tmp
		entrenadores[e.Id] = ent
	} else {
		ent = entrenadores[e.Id]
	}

	t, err := buscarTorneo(req.IdTorneo)
	if err != nil {
		return nil, fmt.Errorf("[LCP] Torneo no encontrado")
	}

	if t.Estado != "Disponible" {
		return &pb.InscripcionResponse{
			Mensaje: "Torneo no está disponible",
			Status:  2,
		}, nil
	}

	if t.Region != e.Region {
		return &pb.InscripcionResponse{
			Mensaje: "Región incompatible con el torneo",
			Status:  3,
		}, nil
	}

	idTorneo, inscrito := inscripciones[e.Id]
	if inscrito && idTorneo != req.IdTorneo {
		return &pb.InscripcionResponse{
			Mensaje: "Ya estás inscrito en otro torneo",
			Status:  4,
		}, nil
	}

	if inscrito && idTorneo == req.IdTorneo {
		return &pb.InscripcionResponse{
			Mensaje: "Ya estás inscrito en este torneo",
			Status:  5,
		}, nil
	}

	if len(t.Entrenadores) >= 2 { // Corrige el sobrecupo
		return &pb.InscripcionResponse{
			Mensaje: "Torneo lleno: ya hay 2 entrenadores",
			Status:  6,
		}, nil
	}

	if ent.Estado == "Suspendido" {
		ent.Suspendido -= 1
		if ent.Suspendido == 0 {
			ent.Estado = "Activo"
		}

		// Notificar al SNP
		enviarRechazoSNP(s.chSNP, ent)

		return &pb.InscripcionResponse{
			Mensaje: "Estás suspendido, no puedes inscribirte",
			Status:  7,
		}, nil
	}

	// Inscribiéndose...
	inscripciones[e.Id] = req.IdTorneo
	t.Entrenadores = append(t.Entrenadores, convertirPbEntrenador(e))

	if len(t.Entrenadores) == 2 {
		t.Estado = "Finalizado"

		puerto := obtenerPuerto(t.Region)
		conn, err := grpc.Dial("gimnasios_container:"+puerto, grpc.WithInsecure())
		if err != nil {
			log.Printf("[LCP] Error conectando al gimnasio %s: %v", t.Region, err)
			return nil, err
		}
		defer conn.Close()

		gimnasio := pb.NewLCP_GimnasioClient(conn)
		_, err = gimnasio.AsignarCombate(context.Background(), &pb.CombateRequest{
			TorneoId:    t.Id,
			Entrenador1: convertirEntrenador(t.Entrenadores[0]),
			Entrenador2: convertirEntrenador(t.Entrenadores[1]),
		})
		if err != nil {
			log.Printf("[LCP] Error asignando combate: %v", err)
		} else {
			// Limpia las inscripciones después del combate para que puedan volver a entrar a un torneo
			delete(inscripciones, t.Entrenadores[0].Id)
			delete(inscripciones, t.Entrenadores[1].Id)
		}
	}

	enviarConfirmacionDeInscripcion(s.chSNP, t, ent)
	return &pb.InscripcionResponse{
		Mensaje: fmt.Sprintf(
			"Inscripción exitosa al torneo %d región %s del entrenador %s",
			t.Id, t.Region, e.Nombre,
		),
		Status: 1, // Ok
	}, nil
}

/* Rechaza la inscripción de un entrenador por suspensión */
func enviarRechazoSNP(ch *amqp.Channel, entrenador *Entrenador) {
	msg := MensajeRechazo{
		TipoMensaje:      "rechazo_lcp",
		IdEntrenador:     entrenador.Id,
		NombreEntrenador: entrenador.Nombre,
		Motivo:           "suspension",
		Fecha:            time.Now().Format("2006-01-02"),
	}

	body, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[LCP] Error serializando mensaje para SNP: %v", err)
		return
	}

	err = ch.Publish(
		"notificaciones",   // Exchange que usa SNP
		"snp.penalizacion", // Routing key que escucha SNP
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		log.Printf("[LCP] Error enviando mensaje a SNP: %v", err)
	} else {
		log.Printf("[LCP] Mensaje enviado a SNP: Entrenador %s suspendido", entrenador.Nombre)
	}
}

/* Obtiene el puerto del gimnasio según la región */
func obtenerPuerto(region string) string {
	switch region {
	case "Kanto":
		return "50052"
	case "Johto":
		return "50053"
	case "Hoenn":
		return "50054"
	case "Sinnoh":
		return "50055"
	case "Unova":
		return "50056"
	default:
		return "50052"
	}
}

/* Busca un torneo por ID */
func buscarTorneo(id int64) (*Torneo, error) {
	for _, t := range torneos {
		if int64(t.Id) == id {
			return t, nil
		}
	}

	return nil, fmt.Errorf("[LCP] Torneo no encontrado")
}

/* Convierte un Torneo a su representación protobuf */
func convertirTorneo(t *Torneo) *pb.Torneo {
	var pbEntrenadores []*pb.Entrenador
	for _, e := range t.Entrenadores {
		pbEntrenadores = append(pbEntrenadores, convertirEntrenador(e))
	}

	return &pb.Torneo{
		Id:        t.Id,
		Region:    t.Region,
		Inscritos: pbEntrenadores,
	}
}

/* Convierte un Entrenador a su representación protobuf */
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

/* Convierte un Entrenador protobuf a su representación interna */
func convertirPbEntrenador(pbE *pb.Entrenador) Entrenador {
	return Entrenador{
		Id:         pbE.Id,
		Nombre:     pbE.Nombre,
		Region:     pbE.Region,
		Ranking:    int(pbE.Ranking),
		Estado:     pbE.Estado,
		Suspendido: int(pbE.Suspendido),
	}
}

/* Crea un nuevo torneo con una región aleatoria */
func crearTorneo() *Torneo {
	region := regionesDisponibles[rand.Intn(len(regionesDisponibles))]

	torneo := &Torneo{Id: torneoIdActual, Region: region, Estado: "Disponible"}

	torneoIdActual++
	return torneo
}

/* Crea torneos automáticamente cada cierto intervalo. */
func iniciarCreacionAutomaticaTorneos(chSNP *amqp.Channel, intervalo time.Duration) {
	ticker := time.NewTicker(intervalo)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			torneo := crearTorneo()
			torneos = append(torneos, torneo)

			log.Printf("[LCP] Se creó un torneo (ID: %d) %s", torneo.Id, torneo.Region)

			enviarNuevoTorneoASNP(chSNP, *torneo)
		}
	}
}

/* ValidarEntrenadores recibe los dos IDs y responde si ambos existen y están activos. */
func (s *serverLCP) ValidarEntrenadores(ctx context.Context, req *pb.ValidarEntrenadoresRequest) (*pb.ValidarEntrenadoresResponse, error) {
	e1, ok1 := entrenadores[req.Id1]
	e2, ok2 := entrenadores[req.Id2]

	log.Printf("[LCP] Solicitud del CDP: Validando entrenadores (%s - %s) y (%s - %s)", e1.Id, e1.Nombre, e2.Id, e2.Nombre)
	// Ambos existen
	if !ok1 || !ok2 || e1 == nil || e2 == nil {
		log.Printf("[LCP] Solicitud del CDP: No se encontró (%s - %s) y/o (%s - %s) en registros", e1.Id, e1.Nombre, e2.Id, e2.Nombre)
		return &pb.ValidarEntrenadoresResponse{
			Activos: false,
		}, nil
	}

	// Ambos deben estar activos
	if e1.Estado != "Activo" || e2.Estado != "Activo" {
		log.Printf("[LCP] Solicitud del CDP: Entre los entrenadores (%s - %s) y (%s - %s), uno o ambos no están activos", e1.Id, e1.Nombre, e2.Id, e2.Nombre)
		return &pb.ValidarEntrenadoresResponse{
			Activos: false,
		}, nil
	}

	log.Printf("[LCP] Solicitud del CDP: Se validaron los entrenadores (%s - %s) y (%s - %s), todo está ok!", e1.Id, e1.Nombre, e2.Id, e2.Nombre)
	// Si está todo bien...
	return &pb.ValidarEntrenadoresResponse{
		Activos: true,
	}, nil
}

/* Escucha los resultados válidos enviados por el CDP y actualiza el ranking de los entrenadores.*/
func escucharResultadosValidos(ch, chSNP *amqp.Channel) {
	err := ch.ExchangeDeclare(
		"resultados_validos", // misma exchange usada por CDP
		"topic",
		true, false, false, false, nil,
	)
	if err != nil {
		log.Fatalf("[LCP] Error declarando exchange: %v", err)
	}

	q, err := ch.QueueDeclare(
		"lcp_resultados", // nombre de la cola del LCP
		true, false, false, false, nil,
	)
	if err != nil {
		log.Fatalf("[LCP] Error declarando cola: %v", err)
	}

	err = ch.QueueBind(
		q.Name,
		"lcp.resultado",      // routing key exacta usada por CDP
		"resultados_validos", // exchange
		false, nil,
	)
	if err != nil {
		log.Fatalf("[LCP] Error haciendo binding: %v", err)
	}

	msgs, err := ch.Consume(
		q.Name, "", true, false, false, false, nil,
	)
	if err != nil {
		log.Fatalf("[LCP] Error al comenzar a consumir mensajes: %v", err)
	}

	go func() {
		for d := range msgs {
			log.Printf("[LCP] Resultado recibido desde CDP: %s", d.Body)

			var resultadoValido ResultadoCombate
			if err := json.Unmarshal(d.Body, &resultadoValido); err != nil {
				log.Printf("[LCP] Error al parsear JSON del resultado: %v", err)
				continue
			}

			actualizarRanking(chSNP, resultadoValido)
		}
	}()
}

/* Actualiza el ranking de los entrenadores según el resultado del combate.*/
func actualizarRanking(chSNP *amqp.Channel, r ResultadoCombate) {
	mu.Lock()
	defer mu.Unlock()

	e1, ok1 := entrenadores[r.IdEntrenador1]
	e2, ok2 := entrenadores[r.IdEntrenador2]

	if !ok1 || !ok2 {
		log.Printf("[LCP] Error: entrenadores no encontrados (%s, %s)", r.IdEntrenador1, r.IdEntrenador2)
		return
	}

	var ganador, perdedor *Entrenador
	if r.IdGanador == e1.Id {
		ganador = e1
		perdedor = e2
	} else {
		ganador = e2
		perdedor = e1
	}

	ganador.Ranking += 25
	perdedor.Ranking -= 25

	log.Printf("[LCP] Ajuste de ranking - Ganador: %s (+25) ELO %d, Perdedor: %s (-25) ELO %d",
		ganador.Nombre, ganador.Ranking, perdedor.Nombre, perdedor.Ranking)

	// Emisión de evento de actualización de ranking al SNP
	enviarActualizacionRankingASNP(chSNP, ganador, r.TorneoId, true, r.Fecha)
	enviarActualizacionRankingASNP(chSNP, perdedor, r.TorneoId, false, r.Fecha)
}

/* Envía un mensaje al SNP con la actualización del ranking de los entrenadores. */
func enviarActualizacionRankingASNP(ch *amqp.Channel, entrenador *Entrenador, torneoId int64, ganador bool, fecha string) {
	msg := MensajeRanking{
		TipoMensaje:      "ranking_actualizado",
		IdEntrenador:     entrenador.Id,
		NombreEntrenador: entrenador.Nombre,
		NuevoRanking:     entrenador.Ranking,
		TorneoId:         torneoId,
		Ganador:          ganador,
		Fecha:            fecha,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[LCP] Error serializando mensaje para SNP: %v", err)
		return
	}

	err = ch.Publish(
		"notificaciones",      // Exchange que usa SNP
		"snp.actualizaciones", // Routing key que escucha SNP
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		log.Printf("[LCP] Error enviando mensaje a SNP: %v", err)
	} else {
		log.Printf("[LCP] Mensaje enviado a SNP: Ranking actualizado para %s", entrenador.Nombre)
	}
}

/* Envía una confirmación de inscripción al SNP cuando un entrenador se inscribe en un torneo. */
func enviarConfirmacionDeInscripcion(ch *amqp.Channel, t *Torneo, ent *Entrenador) {
	msg := MensajeInscripcionConfirmada{
		TipoMensaje:      "inscripcion_confirmada",
		IdTorneo:         t.Id,
		IdEntrenador:     ent.Id,
		NombreEntrenador: ent.Nombre,
		Region:           ent.Region,
		Fecha:            time.Now().Format("2006-01-02"),
	}

	body, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[LCP] Error serializando mensaje de inscripción para SNP: %v", err)
		return
	}

	err = ch.Publish(
		"notificaciones",
		"snp.inscripcion_confirmada",
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		log.Printf("[LCP] Error enviando mensaje de inscripción a SNP: %v", err)
	} else {
		log.Printf("[LCP] Inscripción confirmada enviada a SNP: %s en torneo %d", ent.Nombre, t.Id)
	}
}

/* Envía un mensaje al SNP cuando se crea un nuevo torneo. */
func enviarNuevoTorneoASNP(ch *amqp.Channel, t Torneo) {
	msg := MensajeNuevoTorneo{
		TipoMensaje: "nuevo_torneo",
		IdTorneo:    t.Id,
		Region:      t.Region,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[LCP] Error serializando mensaje para SNP: %v", err)
		return
	}

	err = ch.Publish(
		"notificaciones",        // Exchange que usa SNP
		"snp.torneo_disponible", // Routing key que escucha SNP
		false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		log.Printf("[LCP] Error enviando mensaje a SNP: %v", err)
	} else {
		log.Printf("[LCP] Mensaje enviado a SNP: Nuevo torneo disponible (%d) %s", t.Id, t.Region)
	}
}

// Se entiende como Torneo a 1 sólo combate entre dos entrenadores
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

var (
	torneos             []*Torneo
	torneoIdActual      int64 = 1
	regionesDisponibles       = []string{"Kanto", "Johto", "Unova", "Sinnoh", "Hoenn"}
	entrenadores        map[string]*Entrenador
	inscripciones       = map[string]int64{} // map[IDEntrenador]IdTorneo
	rabbitURL           = "amqp://guest:guest@rabbitmq:5672/"
	mu                  sync.Mutex
)

const (
	intervaloCreacionTorneos = 3 * time.Second
)

func main() {
	rand.Seed(time.Now().UnixNano())

	entrenadores = make(map[string]*Entrenador)

	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		log.Fatalf("[LCP] Error conectando a RabbitMQ: %v", err)
	}
	chCDP, err := conn.Channel()
	if err != nil {
		log.Fatalf("[LCP] Error creando canal RabbitMQ: %v", err)
	}

	chSNP, err := conn.Channel()
	if err != nil {
		log.Fatalf("[LCP] Error creando canal RabbitMQ para SNP: %v", err)
	}

	go iniciarCreacionAutomaticaTorneos(chSNP, intervaloCreacionTorneos)
	go escucharResultadosValidos(chCDP, chSNP)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("[LCP] Error al escuchar el puerto: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterLCP_EntrenadoresServer(grpcServer, &server{
		chSNP: chSNP,
	})
	pb.RegisterCDP_LCPServer(grpcServer, &serverLCP{})

	log.Println("[LCP] Servidor gRPC escuchando...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("[LCP] Error al crear el server: %v", err)
	}
}
