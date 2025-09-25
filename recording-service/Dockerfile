FROM golang:1.25-alpine

# Установка FFmpeg и необходимых пакетов
RUN apk --no-cache add ca-certificates ffmpeg

WORKDIR /app

# Копирование go.mod и go.sum
COPY go.mod go.sum ./
RUN go mod download

# ✅ КРИТИЧНО: Копировать ВСЕ файлы включая папку migrations
COPY . .

# Сборка приложения
RUN go build -o recording-service .

EXPOSE 8080

CMD ["./recording-service"]
