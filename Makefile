GOPROXY ?= https://proxy.golang.org,direct
GO_ENV := GOPROXY=$(GOPROXY)
LOAD_ENV := set -a; [ ! -f .env ] || . ./.env; set +a;

.PHONY: run-mcp run-mcp-stdio run-api run-web web-install build build-linux test test-network clean init init-shop init-regional-sales init-academic import-example check-example migrate-state

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
	$(GO_ENV) go build -o bin/migrate-state ./cmd/migrate-state
	$(GO_ENV) go build -o bin/import-config ./cmd/import-config
	cd web && npm run build


test:
	python3 -m unittest discover -s tests/examples -p '*_test.py'
	node --test deploy/connectors/tests/*.test.cjs
	$(GO_ENV) go test ./...
	cd web && node --test tests/*.test.mjs
	cd web && npm run typecheck

# Linux x86_64 release with pure Go drivers (OceanBase reuses MySQL); no vendor client libraries.
build-linux:
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux-amd64/mcp-server ./cmd/mcp-server
	$(GO_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux-amd64/admin-api ./cmd/admin-api
	cd web && npm run build

# Skips any vendor without DATASCOPE_INTEGRATION_<DRIVER> configuration.
test-network:
	$(LOAD_ENV) $(GO_ENV) go test ./internal/store -run 'Test(NetworkDatabaseIntegration|SchemaJoinNetworkIntegration)' -v -count=1

clean:
	rm -rf bin web/dist

# 已有数据库会拒绝覆盖；使用 -db 指定新文件。
init:
	python3 examples/create_sqlite.py --db data/securities_demo.sqlite3 examples/securities/schema.sqlite.sql examples/securities/seed.sql

init-shop:
	python3 examples/shop/generate.py --db data/shop_demo.sqlite3

# Python 3 标准库生成26张销售业务表；已有文件拒绝覆盖。
init-regional-sales:
	python3 examples/regional-sales/generate.py

# Python 标准库生成高校教务示例；已有文件拒绝覆盖。
init-academic:
	python3 examples/academic/generate.py

.PHONY: init-channel-operations
init-channel-operations:
	python3 examples/channel-operations/generate.py

# EXAMPLE 指向 examples/ 下的声明式配置包；新账号密码由环境变量提供。
EXAMPLE ?= securities
import-example:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/import-config -file "examples/$(EXAMPLE)/bundle.yml"

check-example:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/import-config -file "examples/$(EXAMPLE)/bundle.yml" -check

# 停止 API/MCP 后执行；备份逐库私有数据并迁移配置。
migrate-state:
	$(LOAD_ENV) $(GO_ENV) go run ./cmd/migrate-state
