# Multi-stage Dockerfile: Оптимальная сборка Go-бэкенда для продакшена (размер ~20 МБ)

# 1. Этап сборки (Builder stage)
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Кэширование зависимостей
COPY go.mod ./
RUN go mod download

# Копирование исходного кода
COPY . .

# Сборка статического бинарника без CGO с оптимизацией размера
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/server

# 2. Финальный образ (Production runtime)
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Europe/Moscow

WORKDIR /app

# Копируем скомпилированный бинарник
COPY --from=builder /app/server .

# Копируем конфигурацию и статические файлы сайта
COPY iiko_config.json .
COPY index.html .
COPY assets/ ./assets/
COPY css/ ./css/
COPY js/ ./js/

EXPOSE 8080

CMD ["./server"]

