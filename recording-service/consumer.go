package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

type KafkaConsumer struct {
	reader *kafka.Reader
}

func NewKafkaConsumer() *KafkaConsumer {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "kafka:29092"
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{brokers},
		Topic:       "recording.tasks",
		GroupID:     "recording-workers",
		StartOffset: kafka.LastOffset,
		MinBytes:    10e3, // 10KB
		MaxBytes:    10e6, // 10MB
	})

	return &KafkaConsumer{reader: reader}
}

func (kc *KafkaConsumer) Start(ctx context.Context, jobChannel chan<- RecordingTask) error {
	log.Println("📡 Kafka consumer started, waiting for messages...")

	for {
		select {
		case <-ctx.Done():
			log.Println("📡 Kafka consumer stopping...")
			return kc.reader.Close()

		default:
			message, err := kc.reader.ReadMessage(ctx)
			if err != nil {
				log.Printf("❌ Error reading message: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			var task RecordingTask
			if err := json.Unmarshal(message.Value, &task); err != nil {
				log.Printf("❌ Error unmarshalling message: %v", err)
				continue
			}

			// Фильтрация только stop_recording задач
			if task.Action == "stop_recording" || task.Action == "stop_recording_direct" {
				log.Printf("📨 Received recording task: %s", task.StreamID)

				select {
				case jobChannel <- task:
					log.Printf("✅ Task queued: %s", task.StreamID)
				default:
					log.Printf("⚠️ Job queue full, skipping: %s", task.StreamID)
				}
			} else {
				log.Printf("⏭️ Skipping non-recording task: %s (action: %s)", task.StreamID, task.Action)
			}
		}
	}
}
