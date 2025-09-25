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
	// ✅ ДОБАВЛЯЕМ ПОЛЯ ДЛЯ ПОЛЬЗОВАТЕЛЯ (от main-app)
	UserID   int    `json:"user_id,omitempty"`
	Username string `json:"username,omitempty"`
	Title    string `json:"title,omitempty"`
}

type StreamInfo struct {
	StreamID  string    `json:"stream_id"`
	Status    string    `json:"status"`
	Port      int       `json:"port"`
	SRTAddr   string    `json:"srt_addr"`
	HLSPath   string    `json:"hls_path"`
	StartTime time.Time `json:"start_time"`
	// ✅ ДОБАВЛЯЕМ ПОЛЯ ДЛЯ ПОЛЬЗОВАТЕЛЯ
	UserID   int    `json:"user_id,omitempty"`
	Username string `json:"username,omitempty"`
	Title    string `json:"title,omitempty"`
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
		handleWaitingStatus(notification)
	case "stopped", "error":
		handleStopStatus(notification.StreamID)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
}

// ✅ ОБНОВЛЕННАЯ ФУНКЦИЯ: принимает полную информацию от main-app
func handleWaitingStatus(notification StreamNotification) {
	streamID := notification.StreamID

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

	// ✅ СОХРАНЯЕМ ИНФОРМАЦИЮ О ПОЛЬЗОВАТЕЛЕ ОТ MAIN-APP
	activeStreams[streamID] = &StreamInfo{
		StreamID:  streamID,
		Status:    "waiting",
		Port:      port,
		SRTAddr:   srtAddr,
		HLSPath:   fmt.Sprintf("/hls/%s/stream.m3u8", streamID),
		StartTime: time.Now(),
		UserID:    notification.UserID,
		Username:  notification.Username,
		Title:     notification.Title,
	}

	// ✅ КРИТИЧЕСКИ ВАЖНО: ЗАПУСК HLS UPLOADER
	startHLSUploader(streamID)

	// Уведомить main-app что стрим "live"
	go func() {
		time.Sleep(2 * time.Second)
		notifyMainAppStatusChange(streamID, "running")
	}()

	log.Printf("Started stream %s on port %d (user: %s, id: %d)",
		streamID, port, notification.Username, notification.UserID)
}

// ✅ ИСПРАВЛЕННАЯ ФУНКЦИЯ handleStopStatus
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

	// ✅ ИСПРАВЛЕНИЕ: получить информацию о пользователе из сохраненных данных или main-app
	userID, username, title := getUserInfoFromStream(streamID, stream)

	// ✅ ОТПРАВЛЯЕМ В KAFKA С ПРАВИЛЬНОЙ ИНФОРМАЦИЕЙ О ПОЛЬЗОВАТЕЛЕ
	if kafkaProducer != nil {
		go func() {
			endTime := time.Now()
			startTime := stream.StartTime
			if startTime.IsZero() {
				startTime = endTime.Add(-60 * time.Second) // Fallback
			}
			duration := int(endTime.Sub(startTime).Seconds())

			recordingTask := kafka.RecordingTask{
				StreamID:  streamID,
				UserID:    userID,   // ✅ ПРАВИЛЬНЫЙ USER_ID
				Username:  username, // ✅ ПРАВИЛЬНЫЙ USERNAME
				Title:     title,    // ✅ ПРАВИЛЬНЫЙ TITLE
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
				log.Printf("✅ Recording task sent for stream: %s (user_id: %d, username: %s, duration: %ds)",
					streamID, userID, username, duration)
			}
		}()
	}

	// Удаляем из активных стримов (файлы остаются)
	delete(activeStreams, streamID)

	log.Printf("Stopped stream %s (files preserved, Kafka notified with user info: %s/%d)",
		streamID, username, userID)
}

// ✅ НОВАЯ УЛУЧШЕННАЯ ФУНКЦИЯ: получение информации о пользователе
func getUserInfoFromStream(streamID string, stream *StreamInfo) (int, string, string) {
	// 1. Сначала проверяем сохраненную информацию в StreamInfo
	if stream.UserID > 0 && stream.Username != "" {
		log.Printf("✅ Found user info from StreamInfo: %s (ID: %d)", stream.Username, stream.UserID)
		return stream.UserID, stream.Username, stream.Title
	}

	// 2. Если нет сохраненной информации - запрашиваем у main-app
	log.Printf("🔍 Requesting user info from main-app for stream: %s", streamID)

	client := &http.Client{Timeout: 5 * time.Second}

	// Запрос к main-app для получения информации о задаче по stream_id
	url := fmt.Sprintf("http://main-app:8080/tasks?stream_id=%s", streamID)
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("⚠️ Failed to get task info from main-app: %v", err)
		return 0, "unknown", fmt.Sprintf("Stream %s", streamID)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("⚠️ Main-app returned status %d for stream %s", resp.StatusCode, streamID)
		return 0, "unknown", fmt.Sprintf("Stream %s", streamID)
	}

	var tasks []struct {
		ID       int    `json:"id"`
		StreamID string `json:"stream_id"`
		Name     string `json:"name"`
		UserID   int    `json:"user_id"`
		Username string `json:"username"`
		Status   string `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		log.Printf("⚠️ Failed to decode main-app response: %v", err)
		return 0, "unknown", fmt.Sprintf("Stream %s", streamID)
	}

	// Найти задачу по stream_id
	for _, task := range tasks {
		if task.StreamID == streamID {
			log.Printf("✅ Found user info from main-app: %s (ID: %d)", task.Username, task.UserID)
			return task.UserID, task.Username, task.Name
		}
	}

	log.Printf("⚠️ Stream %s not found in main-app tasks", streamID)
	return 0, "legacy", fmt.Sprintf("Legacy Stream %s", streamID)
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
	// ✅ ИСПРАВЛЕНИЕ: приводим статусы к валидным для main-app
	var mainAppStatus string
	switch status {
	case "live":
		mainAppStatus = "running" // ✅ "live" → "running"
	case "waiting":
		mainAppStatus = "waiting"
	case "stopped":
		mainAppStatus = "stopped"
	case "error":
		mainAppStatus = "error"
	default:
		mainAppStatus = "running" // Fallback
	}

	notification := map[string]interface{}{
		"stream_id": streamID,
		"status":    mainAppStatus, // ✅ ИСПОЛЬЗУЕМ ВАЛИДНЫЙ СТАТУС
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

	log.Printf("Successfully notified main-app: stream %s status changed to %s (mapped from %s)",
		streamID, mainAppStatus, status)

	streamsMux.Lock()
	if stream, exists := activeStreams[streamID]; exists {
		stream.Status = status // Оставляем оригинальный статус в stream-app
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
