# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.25-alpine AS builder

LABEL org.opencontainers.image.source="https://github.com/Ault-Blockchain/ault-miner-node"
LABEL org.opencontainers.image.description="Ault Miner Client"

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Set working directory
WORKDIR /build

# Build argument for private repos
ARG GOPRIVATE=github.com/Ault-Blockchain/*

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies with secret for private repos
RUN --mount=type=secret,id=GH_PAT \
    if [ -f /run/secrets/GH_PAT ]; then \
      git config --global url."https://$(cat /run/secrets/GH_PAT):@github.com/".insteadOf "https://github.com/"; \
    fi && \
    go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -ldflags="-s -w" -o /build/minerd ./cmd

# Final stage
FROM alpine:latest

LABEL org.opencontainers.image.source="https://github.com/Ault-Blockchain/ault-miner-node"
LABEL org.opencontainers.image.description="Ault Miner Client"

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 miner && \
    adduser -D -u 1000 -G miner miner

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/minerd /app/minerd

# Create directories for data
RUN mkdir -p /app/data && \
    chown -R miner:miner /app

# Switch to non-root user
USER miner

# Expose API port
EXPOSE 8080

# Set entrypoint to the miner binary
ENTRYPOINT ["/app/minerd"]

# Default command - mine with non-interactive mode
CMD ["mine", "--yes"]
