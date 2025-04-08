# Makefile for Nexlify project

# 变量定义
PROTOC := protoc
PROTOC_GEN_GO := $(shell go env GOPATH)/bin/protoc-gen-go
PROTOC_GEN_GRPC := $(shell go env GOPATH)/bin/protoc-gen-go-grpc
PROTO_DIR := proto
GO := go
GOFLAGS := -v

# 默认目标
.PHONY: all
all: proto build

# 安装必要的工具
.PHONY: tools
tools:
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 生成 gRPC 和 Protocol Buffers 代码
.PHONY: proto
proto:
	$(PROTOC) --proto_path=$(PROTO_DIR) \
		--go_out=$(PROTO_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_DIR) --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/agent.proto

# 构建服务端
.PHONY: build-server
build-server:
	$(GO) build $(GOFLAGS) -o bin/server ./cmd/server

# 构建 Agent
.PHONY: build-agent
build-agent:
	$(GO) build $(GOFLAGS) -o bin/agent ./cmd/agent

# 构建所有
.PHONY: build
build: build-server build-agent

# 运行服务端
.PHONY: run-server
run-server: build-server
	./bin/server

# 运行 Agent
.PHONY: run-agent
run-agent: build-agent
	./bin/agent

# 清理生成的文件
.PHONY: clean
clean:
	rm -rf bin/*
	rm -f $(PROTO_DIR)/*.pb.go

# 运行测试
.PHONY: test
test:
	$(GO) test ./... -v

# 格式化代码
.PHONY: fmt
fmt:
	$(GO) fmt ./...

# 检查代码规范
.PHONY: lint
lint:
	golangci-lint run

# 初始化项目（安装依赖）
.PHONY: init
init:
	$(GO) mod tidy
	$(GO) get google.golang.org/grpc
	$(GO) get google.golang.org/protobuf
	$(GO) get github.com/jmoiron/sqlx
	$(GO) get github.com/go-redis/redis/v8
	$(GO) get github.com/sirupsen/logrus
	$(GO) get gopkg.in/yaml.v3

# 帮助信息
.PHONY: help
help:
	@echo "Available commands:"
	@echo "  make tools       - Install necessary tools (protoc-gen-go, protoc-gen-go-grpc)"
	@echo "  make proto       - Generate gRPC and Protocol Buffers code"
	@echo "  make build       - Build server and agent binaries"
	@echo "  make build-server - Build server binary"
	@echo "  make build-agent  - Build agent binary"
	@echo "  make run-server  - Run the server"
	@echo "  make run-agent   - Run the agent"
	@echo "  make clean       - Clean generated binaries and proto files"
	@echo "  make test        - Run all tests"
	@echo "  make fmt         - Format Go code"
	@echo "  make lint        - Run linter (requires golangci-lint)"
	@echo "  make init        - Initialize project dependencies"
	@echo "  make help        - Show this help message"