# Multi-stage Dockerfile: Оптимальная сборка Go-бэкенда для продакшена (размер ~20 МБ)

# 1. Этап сборки (Builder stage)
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Кэширование зависимостей модулей Go
COPY go.mod ./
RUN go mod download

# Копирование исходного кода проекта
COPY . .

# Сборка бинарника для Linux без CGO
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/server

# 2. Финальный легковесный runtime-образ
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Europe/Moscow

WORKDIR /app

# Копируем скомпилированный бинарник сервера
COPY --from=builder /app/server .

# Копируем конфигурационный шаблон и статические файлы сайта
COPY iiko_config.example.json ./
COPY index.html demo.html ./
COPY assets/ ./assets/
COPY css/ ./css/
COPY js/ ./js/
COPY data/ ./data/

EXPOSE 8080

CMD ["./server"]
