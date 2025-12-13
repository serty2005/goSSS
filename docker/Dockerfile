# Stage 1: Builder
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Копируем зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем бинарник
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o etalon-server ./cmd/etalon-server

# Stage 2: Runtime
FROM alpine:latest

WORKDIR /app

# Копируем бинарник из билдера
COPY --from=builder /app/etalon-server .

# Копируем папку с мок-данными для сидера, чтобы они были доступны внутри контейнера
COPY --from=builder /app/tools/seeder/mock_data ./tools/seeder/mock_data

RUN mkdir -p ./ftp_cache

# Открываем порт
EXPOSE 8080

# По умолчанию запускаем сервер
CMD ["./etalon-server"]