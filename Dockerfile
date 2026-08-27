# Multi-stage build for orbit-server daemon
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Install build prerequisites
RUN apk add --no-cache git ca-certificates

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Build standalone orbit-server binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/orbit-server ./cmd/orbit-server

# Minimal runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app

COPY --from=builder /bin/orbit-server /usr/local/bin/orbit-server

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/orbit-server"]
