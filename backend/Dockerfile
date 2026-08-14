# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /build

# Install dependencies
RUN apk add --no-cache git

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o cargobay ./cmd/server/main.go

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Create non-root user
RUN addgroup -g 1000 app && adduser -u 1000 -G app -s /bin/sh -D app

COPY --from=builder /build/cargobay ./
COPY --from=builder /build/config.example.yaml ./config.yaml

# Storage directory for local artifact storage, owned by the app user
RUN mkdir -p ./storage && chown -R app:app /app

# Switch to non-root user
USER app

# Expose port
EXPOSE 4500

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:4500/health || exit 1

# Run the server
CMD ["./cargobay"]
