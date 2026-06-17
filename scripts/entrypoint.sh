#!/bin/sh
set -e

# Reconnect to kind network if a cluster exists (survives container restarts)
if docker network ls --format '{{.Name}}' 2>/dev/null | grep -q '^kind$'; then
    docker network connect kind "$(hostname)" 2>/dev/null || true
fi

# Generate dev TLS certs if not present
if [ ! -f /app/certs/tls.crt ]; then
    echo "[entrypoint] Generating dev TLS certs..."
    mkdir -p /app/certs
    openssl req -x509 -newkey rsa:4096 \
        -keyout /app/certs/tls.key \
        -out    /app/certs/tls.crt \
        -days 365 -nodes \
        -subj '/CN=webhook' \
        -addext 'subjectAltName=DNS:webhook,DNS:localhost,IP:127.0.0.1' \
        2>/dev/null
    echo "[entrypoint] Certs written to /app/certs/"
fi

# Generate go.sum if missing
if [ -f /app/go.mod ] && [ ! -f /app/go.sum ]; then
    echo "[entrypoint] Running go mod tidy..."
    cd /app && go mod tidy
fi

exec air -c /app/.air.toml
