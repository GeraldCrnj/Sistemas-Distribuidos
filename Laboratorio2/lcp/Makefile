docker-lcp:
	docker compose up lcp

docker-build-lcp:
	docker compose build lcp

gen-proto-lcp:
	protoc \
	--go_out=. \
	--go-grpc_out=. \
	proto/shared.proto \
	proto/lcp_entrenadores.proto \
	proto/lcp_gimnasios.proto \
	proto/cdp_lcp.proto 
