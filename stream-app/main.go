package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"web/stream-app/kafka"
)

var kafkaProducer *kafka.Producer

func main() {
	// Инициализация MinIO
	if err := initMinIO(); err != nil {
		log.Fatalf("Failed to initialize MinIO: %v", err)
	}

	// Инициализация Kafka
	var err error
	kafkaProducer, err = kafka.NewProducer()
	if err != nil {
		log.Printf("Failed to initialize Kafka: %v", err)
		kafkaProducer = nil
	} else {
		log.Println("✅ Kafka producer initialized successfully")
	}

	// Graceful shutdown для Kafka
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("🛑 Shutting down stream-app...")
		if kafkaProducer != nil {
			kafkaProducer.Close()
		}
		os.Exit(0)
	}()

	// Запускаем восстановление стримов в отдельной горутине
	go func() {
		time.Sleep(2 * time.Second)
		recoverActiveStreams()
	}()

	// Запускаем мониторинг HLS активности
	//go monitorHLSActivity()

	// Существующие API endpoints
	http.HandleFunc("/stream/notify", streamNotifyHandler)
	http.HandleFunc("/stream/status", streamStatusHandler)
	http.HandleFunc("/stream/recover", streamRecoveryHandler)
	http.HandleFunc("/stream/cleanup", streamCleanupHandler)

	// Новые endpoints для интеграции с Kafka
	//http.HandleFunc("/stream/start", streamStartHandler)
	//http.HandleFunc("/stream/stop", streamStopHandler)
	http.HandleFunc("/health", healthHandler)

	// Сервер отдачи HLS плейлистов и сегментов (резервный)
	http.Handle("/hls/", http.StripPrefix("/hls/", http.FileServer(http.Dir("./hls"))))

	addr := ":9090"
	log.Printf("Stream-app listening on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Stream-app failed: %v", err)
	}
}
