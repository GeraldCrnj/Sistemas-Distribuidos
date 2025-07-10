PLAYER1_ID=Player1
PLAYER2_ID=Player2
MATCHMAKER_ADDR=matchmaker:50051

# Run interactivo
docker-run-jugador1:
	docker compose run --rm -it player $(PLAYER1_ID)

docker-run-jugador2:
	docker compose run --rm -it player $(PLAYER2_ID)

# Builds
docker-build-jugador:
	docker compose build player