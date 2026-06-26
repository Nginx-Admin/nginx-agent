# nginx-agent Makefile
BINARY := nginx-agent
PKG := nginx-agent

.PHONY: proto build run vet tidy clean

# 重新生成 protobuf 代码（需要 protoc + protoc-gen-go + protoc-gen-go-grpc）
proto:
	protoc --proto_path=api/proto \
		--go_out=internal/pb --go_opt=paths=source_relative \
		--go-grpc_out=internal/pb --go-grpc_opt=paths=source_relative \
		api/proto/agent.proto

build:
	go build -o bin/$(BINARY) ./cmd/nginx-agent

run:
	go run ./cmd/nginx-agent -config ./config.yaml

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin
