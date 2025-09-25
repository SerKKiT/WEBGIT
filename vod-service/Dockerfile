FROM golang:1.25-alpine

WORKDIR /app

# Устанавливаем необходимые пакеты
RUN apk add --no-cache git ca-certificates


COPY vod-service/ ./vod-service/

# Переходим в папку vod-service
WORKDIR /app/vod-service

# Копируем go.mod и go.sum, загружаем зависимости
RUN go mod tidy
RUN go mod download

# Собираем приложение
RUN go build -o vod-service .

# Устанавливаем права на выполнение
RUN chmod +x ./vod-service

EXPOSE 8081

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

CMD ["./vod-service"]
