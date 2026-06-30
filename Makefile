# nginx-agent Makefile
BINARY := nginx-agent
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X nginx-agent/internal/agent.Version=$(VERSION)
CONFIG ?= ./config.yaml

.PHONY: proto build run vet tidy clean linux-amd64 linux-arm64

# 重新生成 protobuf 代码（需要 protoc + protoc-gen-go + protoc-gen-go-grpc）
proto:
	protoc --proto_path=api/proto \
		--go_out=internal/pb --go_opt=paths=source_relative \
		--go-grpc_out=internal/pb --go-grpc_opt=paths=source_relative \
		api/proto/agent.proto

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/nginx-agent

run:
	go run -ldflags "$(LDFLAGS)" ./cmd/nginx-agent -config $(CONFIG)

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin

linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 ./cmd/nginx-agent

linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 ./cmd/nginx-agent
