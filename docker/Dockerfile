FROM golang:1.26.2-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o etalon-server ./cmd/etalon-server

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=builder /app/etalon-server ./etalon-server

RUN mkdir -p /app/ftp_cache /app/storage/tickets /app/logs /app/tools/seeder/mock_data \
    && chown -R app:app /app

USER app

EXPOSE 8080

ENTRYPOINT ["./etalon-server"]
