docker-gimnasios:
	docker compose up --build gimnasios

gen-proto-gimnasios:
	protoc \
	--go_out=. \
	--go-grpc_out=. \
	proto/shared.proto \
	proto/lcp_gimnasios.proto
