FROM golang:1.25-alpine AS go-builder

WORKDIR /src
RUN apk add --no-cache ca-certificates git tzdata
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" -o /out/n8n-turbo ./cmd/n8n-turbo

FROM alpine:3.22 AS runtime
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG EDITOR_UI_VERSION=2.16.2
ARG NODES_BASE_VERSION=2.16.0
ARG LANGCHAIN_NODES_VERSION=2.16.1

RUN apk add --no-cache ca-certificates tzdata curl go nodejs npm python3 poppler-utils ghostscript imagemagick
WORKDIR /app
COPY --from=go-builder /out/n8n-turbo /app/n8n-turbo
COPY package.json package-lock.json /app/
RUN npm ci --omit=dev --ignore-scripts
RUN mkdir -p /app/ui \
	&& curl -fsSL "https://registry.npmjs.org/n8n-editor-ui/-/n8n-editor-ui-${EDITOR_UI_VERSION}.tgz" | tar -xz -C /tmp \
	&& cp -R /tmp/package/dist/. /app/ui \
	&& rm -rf /tmp/package \
	&& mkdir -p /app/ui/icons/n8n-nodes-base /app/ui/icons/@n8n/n8n-nodes-langchain \
	&& curl -fsSL "https://registry.npmjs.org/n8n-nodes-base/-/n8n-nodes-base-${NODES_BASE_VERSION}.tgz" | tar -xz -C /tmp \
	&& cp -R /tmp/package/dist /app/ui/icons/n8n-nodes-base/dist \
	&& rm -rf /tmp/package \
	&& curl -fsSL "https://registry.npmjs.org/@n8n/n8n-nodes-langchain/-/n8n-nodes-langchain-${LANGCHAIN_NODES_VERSION}.tgz" | tar -xz -C /tmp \
	&& cp -R /tmp/package/dist /app/ui/icons/@n8n/n8n-nodes-langchain/dist \
	&& rm -rf /tmp/package
RUN mkdir -p /app/data /app/logs /app/storage/binary
EXPOSE 5678
ENV N8N_HOST=0.0.0.0
ENV N8N_PORT=5678
ENV N8N_PROTOCOL=http
ENV N8N_PATH=/
ENV N8N_TURBO_FRONTEND_DIR=/app/ui
ENV UI_PATH=/app/ui
ENV DB_TYPE=sqlite
ENV DB_SQLITE_DATABASE=/app/data/database.sqlite
ENV N8N_TURBO_BINARY_DATA_PATH=/app/storage/binary
ENV N8N_DEFAULT_BINARY_DATA_MODE=filesystem
ENV N8N_LOG_LEVEL=info
ENV GENERIC_TIMEZONE=UTC
ENV N8N_EXECUTIONS_DATA_SAVE_ON_ERROR=all
ENV N8N_EXECUTIONS_DATA_SAVE_ON_SUCCESS=all
ENV N8N_EXECUTIONS_DATA_SAVE_MANUAL_EXECUTIONS=true
ENV N8N_EXECUTIONS_DATA_SAVE_ON_PROGRESS=false
ENV N8N_CONCURRENCY_PRODUCTION_LIMIT=0
ENV N8N_TURBO_BUILD=${COMMIT}
ENV N8N_TURBO_VERSION=${VERSION}
ENV N8N_TURBO_BUILD_DATE=${BUILD_DATE}
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 CMD curl -fsS http://127.0.0.1:5678/healthz || exit 1
ENTRYPOINT ["/app/n8n-turbo"]
