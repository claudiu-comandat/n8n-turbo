# n8n-turbo

Standalone Go runtime for an n8n-compatible automation server.

This repository contains everything needed to run the app standalone:

- Go backend source in `cmd/` and `internal/`
- Built frontend bundle in `ui/`
- Docker and Docker Compose files for local or production-style startup

## Local Run

```powershell
$env:N8N_ENCRYPTION_KEY="change-me-generate-32-plus-random-bytes"
$env:N8N_SETUP_EMAIL="owner@n8n.local"
$env:N8N_SETUP_PASSWORD="n8n-turbo"
go run ./cmd/n8n-turbo
```

Open `http://127.0.0.1:5678`.

## Docker

```powershell
docker compose up -d --build
```

The Docker image includes the Go backend, the checked-in `ui/` bundle, Python 3 for Code Python nodes, and Go for Code Go nodes.

Persistent data is stored in Docker volumes for `/app/data`, `/app/logs`, and `/app/storage`.
