# main-app/Dockerfile
FROM golang:1.25-alpine

WORKDIR /app

# Устанавливаем необходимые пакеты
RUN apk add --no-cache git ca-certificates postgresql-client tzdata wget

# Копируем go.mod и go.sum
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем приложение
RUN go build -o main-app .

# Копируем миграции в правильное место
COPY database ./database

# Устанавливаем права на выполнение
RUN chmod +x ./main-app

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/tasks || exit 1

CMD ["./main-app"]
