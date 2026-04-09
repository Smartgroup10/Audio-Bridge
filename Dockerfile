# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /audio-bridge ./cmd/bridge

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -g '' bridge

COPY --from=builder /audio-bridge /usr/local/bin/audio-bridge
COPY configs/config.yaml /etc/audio-bridge/config.yaml

USER bridge

EXPOSE 9092 8080

ENTRYPOINT ["audio-bridge"]
CMD ["-config", "/etc/audio-bridge/config.yaml"]
