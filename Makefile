GOPROXY ?= https://proxy.golang.org,direct
GO_ENV := GOPROXY=$(GOPROXY)
LOAD_ENV := set -a; [ ! -f .env ] || . ./.env; set +a;

.PHONY: run-mcp run-mcp-stdio run-api run-web web-install build build-linux test test-network clean init init-shop install-demo-actions install-demo-masking migrate-state install-demo-sql-examples install-demo-views

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
	$(GO_ENV) go build -o bin/install-demo-actions ./cmd/install-demo-actions
	$(GO_ENV) go build -o bin/install-demo-views ./cmd/install-demo-views
	$(GO_ENV) go build -o bin/install-demo-masking ./cmd/install-demo-masking
	$(GO_ENV) go build -o bin/migrate-state ./cmd/migrate-state
	$(GO_ENV) go build -o bin/gen-data ./cmd/gen-data
	$(GO_ENV) go build -o bin/gen-shop ./cmd/gen-shop
	cd web && npm run build

test:
	node --test deploy/connectors/tests/*.test.cjs
	$(GO_ENV) go test ./...
	cd web && node --test tests/*.test.mjs
	cd web && npm run typecheck

# Linux x86_64 release with pure Go drivers (OceanBase reuses MySQL); no vendor client libraries.
build-linux:
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux-amd64/mcp-server ./cmd/mcp-server
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux-amd64/admin-api ./cmd/admin-api
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux-amd64/install-demo-actions ./cmd/install-demo-actions
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux-amd64/install-demo-views ./cmd/install-demo-views
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux-amd64/install-demo-masking ./cmd/install-demo-masking
	cd web && npm run build

# Skips any vendor without DATASCOPE_INTEGRATION_<DRIVER> configuration.
test-network:
	$(LOAD_ENV) $(GO_ENV) go test ./internal/store -run 'Test(NetworkDatabaseIntegration|SchemaJoinNetworkIntegration)' -v -count=1

clean:
	rm -rf bin web/dist

# 已有数据库会拒绝覆盖；使用 -db 指定新文件。
init:
	$(GO_ENV) go run ./cmd/gen-data

init-shop:
	$(GO_ENV) go run ./cmd/gen-shop

# 为已登记证券演示库的六个字段补充脱敏示例，不覆盖已有规则。
install-demo-masking:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/install-demo-masking

# 显式为已登记证券演示库安装18种 Action，并向三个演示业务账号授权。
install-demo-actions:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/install-demo-actions

# 停止 API/MCP 后执行；备份逐库 management.db 并迁移至分目录 YAML。
migrate-state:
	$(GO_ENV) go run ./cmd/migrate-state

# 显式补齐证券演示库 SQL 示例，保留已有名称或 SQL 相同的示例。
install-demo-sql-examples:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/install-demo-sql-examples

# 仅补充证券业务视图，保留已有视图、动作和授权。
install-demo-views:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/install-demo-views
