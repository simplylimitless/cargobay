# Frontend build stage
FROM node:20-alpine AS frontend-builder

WORKDIR /build

COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install

COPY frontend/ .
RUN npm run build

# Backend build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

# Overwrite the placeholder with the real frontend build output so it gets
# embedded into the binary via //go:embed.
COPY --from=frontend-builder /build/dist ./pkg/webui/dist

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o cargobay ./cmd/server/main.go

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

RUN addgroup -g 1000 app && adduser -u 1000 -G app -s /bin/sh -D app

COPY --from=builder /build/cargobay ./
COPY --from=builder /build/config.example.yaml ./config.yaml

# trivy CLI, used by the backend itself to refresh the vulnerability DB on a
# tunable schedule (trivy server mode has no HTTP control over its own DB
# update cadence -- see backend/pkg/vulnerability/updater.go).
COPY --from=aquasec/trivy:latest /usr/local/bin/trivy /usr/local/bin/trivy

RUN mkdir -p ./storage ./trivy-cache && chown -R app:app /app

USER app

EXPOSE 4500

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:4500/health || exit 1

CMD ["./cargobay"]
