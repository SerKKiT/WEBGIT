package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type ActiveTask struct {
	ID       int    `json:"id"`
	StreamID string `json:"stream_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
}

// Восстановление активных стримов при запуске stream-app
func recoverActiveStreams() {
	log.Println("Starting stream recovery process...")

	// Ждем, пока main-app станет доступен
	if !waitForMainApp() {
		log.Println("Failed to connect to main-app, skipping recovery")
		return
	}

	// Получаем список активных задач
	activeTasks, err := getActiveTasksFromMainApp()
	if err != nil {
		log.Printf("Failed to get active tasks: %v", err)
		return
	}

	if len(activeTasks) == 0 {
		log.Println("No active tasks found for recovery")
		return
	}

	log.Printf("Found %d active tasks for recovery", len(activeTasks))

	// Восстанавливаем каждую активную задачу
	for _, task := range activeTasks {
		err := recoverSingleStream(task)
		if err != nil {
			log.Printf("Failed to recover stream %s: %v", task.StreamID, err)
			continue
		}
		log.Printf("Successfully recovered stream %s (status: %s)", task.StreamID, task.Status)

		// Небольшая пауза между восстановлениями
		time.Sleep(500 * time.Millisecond)
	}

	log.Println("Stream recovery process completed")
}

// Ожидание доступности main-app
func waitForMainApp() bool {
	maxRetries := 30
	retryDelay := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://main-app:8080/tasks")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 400 {
				log.Println("Main-app is available")
				return true
			}
		}

		log.Printf("Waiting for main-app... attempt %d/%d", i+1, maxRetries)
		time.Sleep(retryDelay)
	}

	return false
}

// Получение активных задач от main-app
func getActiveTasksFromMainApp() ([]ActiveTask, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://main-app:8080/tasks/active")
	if err != nil {
		return nil, fmt.Errorf("failed to request active tasks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("main-app returned status %d", resp.StatusCode)
	}

	var tasks []ActiveTask
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return tasks, nil
}

// Восстановление одного стрима
func recoverSingleStream(task ActiveTask) error {
	// Выделяем порт
	port, err := acquirePort()
	if err != nil {
		return fmt.Errorf("failed to acquire port: %v", err)
	}

	// Создаем SRT адрес
	srtAddr := fmt.Sprintf("srt://0.0.0.0:%d?mode=listener&streamid=%s&pkt_size=1316", port, task.StreamID)

	// Создаем информацию о стриме
	streamInfo := &StreamInfo{
		StreamID: task.StreamID,
		Status:   "waiting", // Начинаем с waiting, ffmpeg изменит на running при подключении
		Port:     port,
		SRTAddr:  srtAddr,
		HLSPath:  "/hls/" + task.StreamID + "/stream.m3u8",
	}

	// Добавляем в активные стримы
	streamsMux.Lock()
	activeStreams[task.StreamID] = streamInfo
	streamsMux.Unlock()

	// Запускаем ffmpeg процесс
	if err := startFFmpegProcess(task.StreamID, srtAddr); err != nil {
		// Если не удалось запустить ffmpeg, очищаем ресурсы
		streamsMux.Lock()
		delete(activeStreams, task.StreamID)
		streamsMux.Unlock()
		releasePort(port)
		return fmt.Errorf("failed to start ffmpeg: %v", err)
	}

	// Если задача была в статусе running, но SRT не подключен,
	// переводим в waiting и уведомляем main-app
	if task.Status == "running" {
		go notifyMainAppStatusChange(task.StreamID, "waiting")
	}

	return nil
}
