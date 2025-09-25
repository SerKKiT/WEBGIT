package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
	"web/stream-app/kafka"
)

type StreamNotification struct {
	StreamID string `json:"stream_id"`
	Status   string `json:"status"`
	TaskID   int    `json:"task_id,omitempty"`
}

type StreamInfo struct {
	StreamID  string    `json:"stream_id"`
	Status    string    `json:"status"`
	Port      int       `json:"port"`
	SRTAddr   string    `json:"srt_addr"`
	HLSPath   string    `json:"hls_path"`
	StartTime time.Time `json:"start_time"` // Добавлено для точного времени
}

var (
	activeStreams = make(map[string]*StreamInfo)
	streamsMux    sync.Mutex
)

func streamNotifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var notification StreamNotification
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if notification.StreamID == "" || notification.Status == "" {
		http.Error(w, "Missing stream_id or status", http.StatusBadRequest)
		return
	}

	log.Printf("Received notification: StreamID=%s, Status=%s", notification.StreamID, notification.Status)

	streamsMux.Lock()
	defer streamsMux.Unlock()

	switch notification.Status {
	case "waiting":
		handleWaitingStatus(notification.StreamID)
	case "stopped", "error":
		handleStopStatus(notification.StreamID)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
}

func handleWaitingStatus(streamID string) {
	if stream, exists := activeStreams[streamID]; exists {
		log.Printf("Stream %s already active on port %d", streamID, stream.Port)
		return
	}

	port, err := acquirePort()
	if err != nil {
		log.Printf("Failed to acquire port for stream %s: %v", streamID, err)
		return
	}

	srtAddr := fmt.Sprintf("srt://0.0.0.0:%d?mode=listener&streamid=%s&pkt_size=1316", port, streamID)

	if err := startFFmpegProcess(streamID, srtAddr); err != nil {
		log.Printf("Failed to start ffmpeg for stream %s: %v", streamID, err)
		releasePort(port)
		return
	}

	activeStreams[streamID] = &StreamInfo{
		StreamID:  streamID,
		Status:    "waiting",
		Port:      port,
		SRTAddr:   srtAddr,
		HLSPath:   fmt.Sprintf("/hls/%s/stream.m3u8", streamID),
		StartTime: time.Now(), // Фиксируем точное время старта
	}

	startHLSUploader(streamID)

	// ✅ ИСПРАВЛЕНО: Уведомить main-app что стрим "live"
	go func() {
		time.Sleep(2 * time.Second) // Дать время FFmpeg запуститься
		notifyMainAppStatusChange(streamID, "live")
	}()

	log.Printf("Started stream %s on port %d with MinIO integration", streamID, port)
}

func handleStopStatus(streamID string) {
	stream, exists := activeStreams[streamID]
	if !exists {
		log.Printf("Stream %s not found for stopping", streamID)
		return
	}

	// Останавливаем ffmpeg процесс
	stopFFmpegProcess(streamID)

	// Освобождаем порт
	releasePort(stream.Port)

	// ✅ ЕДИНСТВЕННАЯ отправка в Kafka
	if kafkaProducer != nil {
		go func() {
			endTime := time.Now()
			startTime := stream.StartTime // Используем точное время старта
			if startTime.IsZero() {
				startTime = endTime.Add(-60 * time.Second) // Fallback
			}
			duration := int(endTime.Sub(startTime).Seconds())

			recordingTask := kafka.RecordingTask{
				StreamID:  streamID,
				Action:    "stop_recording",
				HLSPath:   fmt.Sprintf("/hls/%s/", streamID),
				StartTime: startTime,
				EndTime:   endTime,
				Duration:  duration,
				Status:    "completed",
				Timestamp: time.Now(),
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := kafkaProducer.SendRecordingTask(ctx, recordingTask); err != nil {
				log.Printf("❌ Failed to send recording task: %v", err)
			} else {
				log.Printf("✅ Recording task sent for stream: %s (duration: %ds)", streamID, duration)
			}
		}()
	}

	// Удаляем из активных стримов (файлы остаются)
	delete(activeStreams, streamID)

	log.Printf("Stopped stream %s (files preserved, Kafka notified)", streamID)
}

func streamStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	streamsMux.Lock()
	defer streamsMux.Unlock()

	var result []*StreamInfo
	for _, s := range activeStreams {
		result = append(result, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func streamRecoveryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go recoverActiveStreams()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "recovery started"})
}

func notifyMainAppStatusChange(streamID, status string) {
	notification := map[string]interface{}{
		"stream_id": streamID,
		"status":    status,
	}

	jsonData, err := json.Marshal(notification)
	if err != nil {
		log.Printf("Failed to marshal status change notification: %v", err)
		return
	}

	url := "http://main-app:8080/tasks/update_status_by_stream"
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create PUT request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to notify main-app about status change: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("Main-app returned error status %d for status change", resp.StatusCode)
		return
	}

	log.Printf("Successfully notified main-app: stream %s status changed to %s", streamID, status)

	streamsMux.Lock()
	if stream, exists := activeStreams[streamID]; exists {
		stream.Status = status
	}
	streamsMux.Unlock()
}

func streamCleanupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		StreamID string `json:"stream_id"`
		Action   string `json:"action"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.StreamID == "" || req.Action == "" {
		http.Error(w, "Missing stream_id or action", http.StatusBadRequest)
		return
	}

	log.Printf("Received cleanup request: StreamID=%s, Action=%s", req.StreamID, req.Action)

	if req.Action == "cleanup_folder" {
		if err := cleanupLocalHLSFolder(req.StreamID); err != nil {
			log.Printf("Failed to cleanup local folder for stream %s: %v", req.StreamID, err)
			http.Error(w, "Failed to cleanup folder", http.StatusInternalServerError)
			return
		}
		log.Printf("Successfully cleaned up local folder for stream %s", req.StreamID)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "cleaned"})
}

// ✅ Health check (оставляем для мониторинга)
func healthHandler(w http.ResponseWriter, r *http.Request) {
	streamsMux.Lock()
	activeCount := len(activeStreams)
	streamsMux.Unlock()

	kafkaStatus := "disconnected"
	if kafkaProducer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := kafkaProducer.TestConnection(ctx); err == nil {
			kafkaStatus = "connected"
		}
	}

	health := map[string]interface{}{
		"status":         "ok",
		"active_streams": activeCount,
		"kafka_status":   kafkaStatus,
		"timestamp":      time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// ❌ УДАЛЕНЫ дублированные handlers:
// - streamStartHandler (дублировал handleWaitingStatus)
// - streamStopHandler (дублировал handleStopStatus + создавал двойную отправку)
