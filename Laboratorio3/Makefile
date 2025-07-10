PROTOC = protoc
PROTOC_FLAGS = --go_out=. --go-grpc_out=.

PROTO_SUFFIX = /proto/*.proto

ADMIN_PROTOS = administrator$(PROTO_SUFFIX)
MATCHMAKER_PROTOS = matchmaker$(PROTO_SUFFIX)
PLAYER_PROTOS = player$(PROTO_SUFFIX)
GAMESERVERS_PROTOS = gameservers$(PROTO_SUFFIX)

.PHONY: gen-protos clean-protos

gen-protos:
	# $(PROTOC) $(PROTOC_FLAGS) $(ADMIN_PROTOS)
	$(PROTOC) $(PROTOC_FLAGS) $(MATCHMAKER_PROTOS)
	$(PROTOC) $(PROTOC_FLAGS) $(PLAYER_PROTOS)
	$(PROTOC) $(PROTOC_FLAGS) $(GAMESERVERS_PROTOS)

clean-protos:
	find . -type d -name 'compiled' -exec rm -r {} +