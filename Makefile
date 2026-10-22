GOPROXY ?= https://goproxy.cn,direct
GO_ENV := GOPROXY=$(GOPROXY)
LOAD_ENV := set -a; [ ! -f .env ] || . ./.env; set +a;

.PHONY: run-mcp run-mcp-stdio run-api run-web web-install build test clean init

# 三个服务分别在三个终端启动：MCP server、后台 API、前端。
run-mcp:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/mcp-server

run-mcp-stdio:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/mcp-server -transport stdio

run-api:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/admin-api

run-web:
	$(LOAD_ENV) cd web && npm run dev

web-install:
	cd web && npm install

build:
	$(GO_ENV) go build -o bin/mcp-server ./cmd/mcp-server
	$(GO_ENV) go build -o bin/admin-api ./cmd/admin-api
	$(GO_ENV) go build -o bin/gen-data ./cmd/gen-data
	cd web && npm run build

test:
	$(GO_ENV) go test ./...
	cd web && npm run typecheck

clean:
	rm -rf bin web/dist

# 已有数据库会拒绝覆盖；使用 -db 指定新文件。
init:
	$(GO_ENV) go run ./cmd/gen-data
