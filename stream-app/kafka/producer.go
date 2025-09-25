package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

type Producer struct {
	writer *kafka.Writer
}

type RecordingTask struct {
	StreamID     string    `json:"stream_id"`
	UserID       int       `json:"user_id"`  // ✅ ДОБАВЛЕНО
	Username     string    `json:"username"` // ✅ ДОБАВЛЕНО
	Title        string    `json:"title"`
	Action       string    `json:"action"`   // "stop_recording", "start_recording"
	HLSPath      string    `json:"hls_path"` // путь к HLS сегментам
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Duration     int       `json:"duration_seconds"`
	FileSize     int64     `json:"file_size_bytes,omitempty"`
	SegmentCount int       `json:"segment_count,omitempty"`
	Status       string    `json:"status"` // "completed", "failed"
	Timestamp    time.Time `json:"timestamp"`
	ErrorMsg     string    `json:"error_message,omitempty"`
}

// Создать новый producer
func NewProducer() (*Producer, error) {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "kafka:29092"
	}

	log.Printf("🔗 Connecting to Kafka brokers: %s", brokers)

	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokers),
		Topic:                  "recording.tasks",
		Balancer:               &kafka.LeastBytes{},
		RequiredAcks:           kafka.RequireOne,
		Async:                  false, // Синхронная отправка для надёжности
		WriteTimeout:           10 * time.Second,
		ReadTimeout:            10 * time.Second,
		AllowAutoTopicCreation: true,
	}

	return &Producer{writer: writer}, nil
}

// Отправить задачу на обработку записи
func (p *Producer) SendRecordingTask(ctx context.Context, task RecordingTask) error {
	// Установить timestamp если не задан
	if task.Timestamp.IsZero() {
		task.Timestamp = time.Now()
	}

	// Валидация обязательных полей
	if task.StreamID == "" {
		return fmt.Errorf("stream_id is required")
	}
	if task.Action == "" {
		return fmt.Errorf("action is required")
	}

	// ✅ Логируем информацию о пользователе
	if task.UserID > 0 {
		log.Printf("📨 Sending recording task for authorized stream: %s (user: %s, id: %d)",
			task.StreamID, task.Username, task.UserID)
	} else {
		log.Printf("📨 Sending recording task for legacy stream: %s", task.StreamID)
	}

	// Сериализовать в JSON
	taskBytes, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// Отправить сообщение
	message := kafka.Message{
		Key:   []byte(task.StreamID), // Партиционирование по StreamID
		Value: taskBytes,
		Time:  task.Timestamp,
		Headers: []kafka.Header{
			{Key: "action", Value: []byte(task.Action)},
			{Key: "source", Value: []byte("stream-app")},
			{Key: "user_id", Value: []byte(fmt.Sprintf("%d", task.UserID))}, // ✅ ДОБАВЛЕНО
		},
	}

	err = p.writer.WriteMessages(ctx, message)
	if err != nil {
		log.Printf("❌ Failed to send recording task for stream %s: %v", task.StreamID, err)
		return fmt.Errorf("failed to write to kafka: %w", err)
	}

	log.Printf("✅ Recording task sent: stream=%s, user=%s(%d), action=%s, duration=%ds",
		task.StreamID, task.Username, task.UserID, task.Action, task.Duration)
	return nil
}

// Отправить событие о статусе
func (p *Producer) SendStatusEvent(ctx context.Context, streamID, status, message string) error {
	event := map[string]interface{}{
		"stream_id": streamID,
		"status":    status,
		"message":   message,
		"timestamp": time.Now(),
		"source":    "stream-app",
	}

	eventBytes, err := json.Marshal(event)
	if err != nil {
		return err
	}

	// Отправляем в топик events
	writer := &kafka.Writer{
		Addr:                   p.writer.Addr,
		Topic:                  "recording.events",
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
	}
	defer writer.Close()

	return writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(streamID),
		Value: eventBytes,
		Time:  time.Now(),
	})
}

// Проверить подключение к Kafka
func (p *Producer) TestConnection(ctx context.Context) error {
	testMessage := kafka.Message{
		Key:   []byte("health-check"),
		Value: []byte(`{"type":"health-check","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`),
	}

	return p.writer.WriteMessages(ctx, testMessage)
}

// Закрыть producer
func (p *Producer) Close() error {
	if p.writer != nil {
		return p.writer.Close()
	}
	return nil
}
