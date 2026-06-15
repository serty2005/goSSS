FROM golang:1.26.2-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o agent-gateway ./cmd/agent-gateway

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=builder /app/agent-gateway ./agent-gateway

RUN chown -R app:app /app

USER app

EXPOSE 8090

ENTRYPOINT ["./agent-gateway"]
