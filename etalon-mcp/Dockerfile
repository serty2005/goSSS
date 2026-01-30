# Этап сборки
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Копируем файлы зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем бинарник (статически слинкованный)
RUN CGO_ENABLED=0 GOOS=linux go build -o etalon-mcp .

# Финальный этап
FROM alpine:latest

WORKDIR /app

# Устанавливаем сертификаты (на случай если MCP захочет дергать внешние API, хотя здесь это не нужно, но полезно)
RUN apk --no-cache add ca-certificates

# Копируем бинарник из этапа сборки
COPY --from=builder /app/etalon-mcp .

# Создаем директорию для монтирования кода ( volume mount point)
RUN mkdir -p /app/codebase

# Указываем порт (для документации, compose сам маппит)
EXPOSE 8080

# Запускаем сервер
CMD ["./etalon-mcp"]