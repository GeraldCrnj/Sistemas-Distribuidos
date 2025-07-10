
docker-cdp:
	docker compose up cdp

docker-build-cdp:
	docker compose build cdp

gen-proto-cdp:
	protoc \
	--go_out=. \
	--go-grpc_out=. \
	proto/cdp_lcp.proto