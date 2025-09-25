FROM golang:1.25-alpine

RUN apk add --no-cache ffmpeg

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN mkdir -p ./hls && chmod -R 777 ./hls

RUN go build -o stream-app .

EXPOSE 9090

CMD ["./stream-app"]
