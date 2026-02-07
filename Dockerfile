# === Build Stage ===
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o /talkeq .

# === Runtime Stage ===
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -h /app talkeq

WORKDIR /app
COPY --from=builder /talkeq /app/talkeq

USER talkeq

ENTRYPOINT ["/app/talkeq"]
