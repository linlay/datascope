FROM harbor.gtjaqh.io/library/node:22.9.0 AS web-build
WORKDIR /src/web
ARG NPM_REGISTRY=http://nexus.gtjaqh.net/repository/npm-group/
ARG VITE_BASE_PATH=/datascope/
ENV NPM_CONFIG_REGISTRY=${NPM_REGISTRY} \
    VITE_BASE_PATH=${VITE_BASE_PATH}
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM harbor.gtjaqh.io/library/golang:1.25-alpine AS go-build
WORKDIR /src
ARG GOPROXY=https://mirrors.gtjaqh.cloud/goproxy
ENV CGO_ENABLED=0 \
    GOPROXY=${GOPROXY} \
    GOSUMDB=off \
    GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN go build -trimpath -ldflags="-s -w" -o /out/mcp-server ./cmd/mcp-server \
    && go build -trimpath -ldflags="-s -w" -o /out/admin-api ./cmd/admin-api \
    && go build -trimpath -ldflags="-s -w" -o /out/gen-data ./cmd/gen-data

FROM harbor.gtjaqh.io/library/nginx:1.28.0-alpine AS runtime
RUN rm -f /etc/nginx/conf.d/default.conf

WORKDIR /app
COPY --from=go-build /out/ /usr/local/bin/
COPY --from=web-build /src/web/dist/ /usr/share/nginx/html/
COPY config/ ./config/
COPY deploy/connectors/ ./deploy/connectors/
COPY docs/ ./docs/
COPY fixtures/ ./fixtures/
COPY sql/ ./sql/
COPY deploy/nginx.docker.conf /etc/nginx/conf.d/datascope.conf
COPY deploy/nginx.proxy_params /etc/nginx/proxy_params
COPY deploy/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

ENV MANAGEMENT_DB_PATH=/app/data/management.sqlite \
    AUDIT_DB_PATH=/app/data/audit.sqlite \
    DATABASE_ALLOWED_ROOTS=/app/data \
    SQLITE_PATH=/app/data/securities_demo.sqlite3 \
    REWRITE_CONFIG=/app/config/rewrite.yml \
    DATABASE_CONFIG_DIR=/app/config/databases \
    DATABASE_CATALOG=/app/data/databases.json \
    OAUTH_KEY_PATH=/app/data/oauth/signing.pem \
    OAUTH_STATE_PATH=/app/data/oauth/state.sqlite3 \
    SERVER_ADDR=127.0.0.1:18080 \
    ADMIN_API_ADDR=127.0.0.1:18081 \
    MCP_SERVER_URL=http://127.0.0.1:18080/mcp \
    MCP_RESOURCE_URL=http://localhost:8080/mcp \
    OAUTH_ISSUER=http://localhost:8080 \
    SESSION_COOKIE_SECURE=false \
    INITIALIZE_DEMO_DATA=true

VOLUME ["/app/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/bin/sh", "/usr/local/bin/docker-entrypoint.sh"]
