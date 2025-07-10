FILE_NAME ?= entrenadores_grande.json

docker-entrenadores:
	FILE_NAME=$(FILE_NAME) docker compose run --rm -it entrenadores

docker-build-entrenadores:
	FILE_NAME=$(FILE_NAME) docker compose build entrenadores

gen-proto-entrenadores:
	protoc \
	--go_out=. \
	--go-grpc_out=. \
	proto/shared.proto \
	proto/lcp_entrenadores.proto
