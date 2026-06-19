# Production Deployment Checklist

- Generate `N8N_ENCRYPTION_KEY` with at least 32 random bytes and never rotate it without backing up encrypted credentials.
- Set `N8N_SETUP_PASSWORD` and `POSTGRES_PASSWORD` to unique secrets before starting containers.
- Set `WEBHOOK_URL` and `N8N_EDITOR_BASE_URL` to the public HTTPS URL.
- Put TLS termination in front of the service or run behind a trusted reverse proxy.
- Persist `/app/data`, `/app/logs`, and `/app/storage` volumes.
- Back up SQLite or Postgres data and binary storage together.
- Keep `N8N_TURBO_FRONTEND_DIR=/app/ui` unless a custom UI volume is mounted.
- Use `docker compose --env-file .env up -d` for single-instance SQLite.
- Use `docker compose -f docker-compose.prod.yml --env-file .env up -d` for Postgres-backed production deployments.
- Verify `curl -fsS http://127.0.0.1:5678/healthz` inside the container.
- Enable metrics only behind internal network access.
- Keep the checked-in `ui/` directory in sync with the n8n-turbo frontend bundle used for release builds.
