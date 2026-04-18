# ==========================================
# Phase 1: Frontend Builder
# ==========================================
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend

# Copy frontend source and install dependencies
COPY frontend/package*.json ./
RUN npm ci || npm install
COPY frontend/ ./

# Build the frontend
RUN npm run build


# ==========================================
# Phase 2: Go Backend Builder
# ==========================================
FROM golang:1.25-alpine AS builder
WORKDIR /app

# Install ca-certificates and tzdata
RUN apk --no-cache add ca-certificates tzdata

# Cache Go modules first for faster builds
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the backend source code
COPY . .

# Copy the compiled static assets from the frontend builder stage
COPY --from=frontend-builder /app/internal/web/frontend_dist ./internal/web/frontend_dist

# Build the main binary statically
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o hijarr ./cmd/hijarr


# ==========================================
# Phase 3: Final Production Image
# ==========================================
FROM alpine:latest
WORKDIR /app

# Add tzdata for scheduler timezone support, ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata curl

# Copy the compiled binary from step 2
COPY --from=builder /app/hijarr /app/hijarr

# The application runs on 8001
EXPOSE 8001

# Default Environment Variables
ENV PORT="8001" \
    PROWLARR_TARGET_URL="" \

    PROWLARR_API_KEY="" \
    SONARR_URL="" \
    SONARR_API_KEY="" \
    TMDB_API_KEY="" \
    TARGET_LANGUAGE="zh-CN" \
    TVDB_LANGUAGE="zho" \
    CACHE_DB_PATH="/data/hijarr.db"

VOLUME ["/data"]

ENTRYPOINT ["/app/hijarr"]
