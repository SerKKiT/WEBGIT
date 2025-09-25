FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates git

WORKDIR /app

# Копирование go.mod и go.sum
COPY auth-service/go.mod auth-service/go.sum ./
RUN go mod download

# Копирование исходного кода
COPY auth-service/ ./

# Сборка приложения
RUN go build -o auth-service .

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates curl tzdata && \
    adduser -D -s /bin/sh appuser

WORKDIR /app

COPY --from=builder /app/auth-service .
COPY --from=builder /app/migrations ./migrations/

RUN chown -R appuser:appuser /app
USER appuser

EXPOSE 8082

CMD ["./auth-service"]
