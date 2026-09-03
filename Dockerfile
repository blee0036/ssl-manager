# Multi-stage build for SSL Manager

# Stage 1: Build Frontend
# Run build stages on the native BuildKit host platform. Without this, the
# linux/arm64 matrix job runs Node/Go under QEMU on GitHub amd64 runners.
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend-builder

WORKDIR /app/webui
RUN corepack enable && corepack prepare pnpm@9 --activate

# Copy package files first for better caching
COPY webui/package.json webui/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# Copy source and build
COPY webui/ ./
RUN pnpm build

# Stage 2: Build Web Backend and Agent
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /src
ARG BUILDPLATFORM
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=0.0.0

# Install build dependencies
RUN apk add --no-cache make

# Copy go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy frontend build artifacts into the Go build context
# This is required for //go:embed dist/* in webui/embed.go
COPY --from=frontend-builder /app/webui/dist ./webui/dist

# Build the Web binary for the target image architecture, and include Agent
# binaries for all supported target-machine platforms with version injection.
RUN BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ') && \
    AGENT_LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildvcs=false -ldflags="-s -w" -o /out/ssl-manager-web ./cmd/web && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -ldflags="${AGENT_LDFLAGS}" -o /out/ssl-manager-agent-linux-amd64 ./cmd/agent && \
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags="${AGENT_LDFLAGS}" -o /out/ssl-manager-agent-linux-arm64 ./cmd/agent && \
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -buildvcs=false -ldflags="${AGENT_LDFLAGS}" -o /out/ssl-manager-agent-darwin-amd64 ./cmd/agent && \
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -buildvcs=false -ldflags="${AGENT_LDFLAGS}" -o /out/ssl-manager-agent-darwin-arm64 ./cmd/agent && \
    echo "${VERSION}" > /out/agent-version.txt

# Stage 3: Runtime image
FROM alpine:3.20

RUN apk add --no-cache \
    ca-certificates \
    certbot \
    certbot-dns-cloudflare \
    curl \
    su-exec \
    tzdata

WORKDIR /app

# With no explicit certbot.data_dir, keep Certbot state in the persistent
# application volume instead of trying to write the host-native /etc path.
ENV SSL_MANAGER_CERTBOT_DATA_DIR=/app/data/certbot

# Copy binaries
COPY --from=builder /out/ssl-manager-web /app/ssl-manager-web
COPY --from=builder /out/ssl-manager-agent-linux-amd64 /app/bin/ssl-manager-agent-linux-amd64
COPY --from=builder /out/ssl-manager-agent-linux-arm64 /app/bin/ssl-manager-agent-linux-arm64
COPY --from=builder /out/ssl-manager-agent-darwin-amd64 /app/bin/ssl-manager-agent-darwin-amd64
COPY --from=builder /out/ssl-manager-agent-darwin-arm64 /app/bin/ssl-manager-agent-darwin-arm64
COPY --from=builder /out/agent-version.txt /app/bin/agent-version.txt
COPY docker-entrypoint.sh /app/docker-entrypoint.sh

# Copy web assets (templates and static files are embedded, but keep for reference)
# The Go binary embeds webui/dist/ via embed.FS, so no separate copy needed.

# Create data directory and certbot working directory (writable by sslmanager)
RUN mkdir -p /app/data /app/data/certbot /app/data/certbot/work /app/data/certbot/logs /app/bin && \
    chmod 700 /app/data && \
    chmod +x /app/docker-entrypoint.sh

# Expose default port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Volume for persistent data (SQLite DB, certificates, config, JWT secret)
VOLUME ["/app/data"]

# Create the runtime user, but stay root at container start so the
# entrypoint can fix up bind-mount/volume ownership on /app/data before
# dropping privileges. Without this, a host-owned bind mount (e.g.
# `-v $(pwd)/ssl-manager-data:/app/data`) overrides the image's chown and
# SQLite fails with the misleading "out of memory (14)" error because the
# non-root user can't actually write to the directory.
RUN adduser -D -H -u 1000 sslmanager && \
    chown -R sslmanager:sslmanager /app

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/ssl-manager-web"]
