# Multi-stage Dockerfile for SnapHaven Server (Headless / NAS / Container)
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Copy Go module specifications
COPY server/go.mod server/go.sum ./
RUN go mod download

# Copy server source code
COPY server/ ./

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Build optimized static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildTime=${BUILD_DATE}'" -o /build/snaphaven-server .

# Final minimal production container
FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy compiled binary from builder
COPY --from=builder /build/snaphaven-server /app/snaphaven-server

# Expose single unified port (mTLS gRPC + Web Setup Dashboard)
EXPOSE 50005

# Setup default directories for photos and configuration/certs
RUN mkdir -p /data/photos && ln -s /data/photos /photos
VOLUME ["/data/photos", "/config"]

ENV HEADLESS=true \
    SNAP_DIR=/data/photos \
    SNAP_PORT=:50005

ENTRYPOINT ["/app/snaphaven-server"]
CMD ["--headless", "--dir", "/data/photos", "--certdir", "/config/certs"]
