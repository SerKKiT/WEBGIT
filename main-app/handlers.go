package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Message struct {
	ID   int    `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
}

type Task struct {
	ID       int       `json:"id,omitempty"`
	StreamID string    `json:"stream_id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created,omitempty"`
	Updated  time.Time `json:"updated,omitempty"`
	Status   string    `json:"status"`
}

// Адрес stream-app из переменных окружения
var streamAppURL = fmt.Sprintf("http://%s:%s", os.Getenv("STREAMAPP_HOST"), os.Getenv("STREAMAPP_PORT"))

// Генерация StreamID: случайная строка из цифр, букв и дефисов
func generateStreamID() (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789-"
	const length = 12
	bytes := make([]byte, length)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	for i := 0; i < length; i++ {
		bytes[i] = letters[int(bytes[i])%len(letters)]
	}
	return fmt.Sprintf("%s%s-%s-%s", string(bytes[0:3]), string(bytes[3:6]), string(bytes[6:9]), string(bytes[9:12])), nil
}

// Хэндлеры для задач (tasks)
func GetTasksHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(context.Background(), "SELECT id, streamid, name, created, updated, status FROM Tasks")
	if err != nil {
		http.Error(w, "Failed to fetch tasks", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.StreamID, &t.Name, &t.Created, &t.Updated, &t.Status); err != nil {
			http.Error(w, "Error scanning task", http.StatusInternalServerError)
			return
		}
		tasks = append(tasks, t)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func CreateTaskHandler(w http.ResponseWriter, r *http.Request) {
	var t Task
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if t.Name == "" {
		http.Error(w, "Missing task name", http.StatusBadRequest)
		return
	}

	streamID, err := generateStreamID()
	if err != nil {
		http.Error(w, "Failed to generate streamID", http.StatusInternalServerError)
		return
	}
	t.StreamID = streamID
	t.Status = "stopped"

	err = db.QueryRow(context.Background(),
		`INSERT INTO Tasks (streamid, name, status) VALUES ($1, $2, $3) RETURNING id, created, updated`,
		t.StreamID, t.Name, t.Status).Scan(&t.ID, &t.Created, &t.Updated)
	if err != nil {
		http.Error(w, "Failed to create task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func UpdateTaskHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid id parameter", http.StatusBadRequest)
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !isValidStatus(req.Status) {
		http.Error(w, "Invalid status value", http.StatusBadRequest)
		return
	}

	// Получаем stream_id задачи из базы для уведомления stream-app
	var streamID string
	err = db.QueryRow(context.Background(), "SELECT streamid FROM Tasks WHERE id=$1", id).Scan(&streamID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	cmdTag, err := db.Exec(context.Background(),
		`UPDATE Tasks SET status=$1, updated=NOW() WHERE id=$2`,
		req.Status, id)
	if err != nil {
		http.Error(w, "Failed to update task", http.StatusInternalServerError)
		return
	}
	if cmdTag.RowsAffected() == 0 {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	// Уведомляем stream-app о смене статуса
	if err := notifyStreamApp(streamID, req.Status, id); err != nil {
		log.Printf("Failed to notify stream-app: %v", err)
		// Не возвращаем ошибку, чтобы не блокировать обновление задачи
	}

	w.WriteHeader(http.StatusNoContent)
}

func DeleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid id parameter", http.StatusBadRequest)
		return
	}

	// Получаем streamID и статус задачи перед удалением
	var streamID, status string
	err = db.QueryRow(context.Background(),
		"SELECT streamid, status FROM Tasks WHERE id=$1", id).Scan(&streamID, &status)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	// Если задача активна, сначала останавливаем стрим
	if status == "waiting" || status == "running" {
		log.Printf("Stopping active stream %s before deletion", streamID)
		if err := notifyStreamApp(streamID, "stopped", id); err != nil {
			log.Printf("Failed to stop stream before deletion: %v", err)
		}
		time.Sleep(2 * time.Second) // Даем время на остановку
	}

	// Удаляем задачу из базы данных
	cmdTag, err := db.Exec(context.Background(), "DELETE FROM Tasks WHERE id=$1", id)
	if err != nil {
		http.Error(w, "Failed to delete task", http.StatusInternalServerError)
		return
	}
	if cmdTag.RowsAffected() == 0 {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	// Запрашиваем удаление ТОЛЬКО локальной папки в stream-app контейнере
	if err := notifyStreamAppCleanupFolder(streamID); err != nil {
		log.Printf("Failed to request folder cleanup for stream %s: %v", streamID, err)
	}

	log.Printf("Deleted task %d and requested local folder cleanup for stream %s", id, streamID)
	w.WriteHeader(http.StatusNoContent)
}

// Функция уведомления stream-app для очистки локальной папки
func notifyStreamAppCleanupFolder(streamID string) error {
	payload := map[string]string{
		"stream_id": streamID,
		"action":    "cleanup_folder",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal cleanup payload: %v", err)
	}

	url := "http://stream-app:9090/stream/cleanup"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create cleanup request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send cleanup request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("cleanup request failed with status %d", resp.StatusCode)
	}

	log.Printf("Successfully requested local folder cleanup for stream %s", streamID)
	return nil
}

func isValidStatus(s string) bool {
	switch s {
	case "stopped", "waiting", "running", "error":
		return true
	default:
		return false
	}
}

func notifyStreamApp(streamID, status string, taskID int) error {
	notification := map[string]interface{}{
		"stream_id": streamID,
		"status":    status,
		"task_id":   taskID,
	}

	jsonData, err := json.Marshal(notification)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post("http://stream-app:9090/stream/notify", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("stream-app returned status %s", resp.Status)
	}
	return nil
}

// Обновление статуса задачи по StreamID (для уведомлений от stream-app)
func UpdateTaskStatusByStreamHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		StreamID string `json:"stream_id"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if !isValidStatus(req.Status) {
		http.Error(w, "Invalid status value", http.StatusBadRequest)
		return
	}

	cmdTag, err := db.Exec(context.Background(),
		`UPDATE Tasks SET status=$1, updated=NOW() WHERE streamid=$2`,
		req.Status, req.StreamID)
	if err != nil {
		http.Error(w, "Failed to update task", http.StatusInternalServerError)
		return
	}
	if cmdTag.RowsAffected() == 0 {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	log.Printf("Updated task status to %s for stream %s", req.Status, req.StreamID)
	w.WriteHeader(http.StatusNoContent)
}

// Получение активных задач (waiting/running) для восстановления в stream-app
func GetActiveTasksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query(context.Background(),
		"SELECT id, streamid, name, status FROM Tasks WHERE status IN ('waiting', 'running')")
	if err != nil {
		http.Error(w, "Failed to fetch active tasks", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.StreamID, &t.Name, &t.Status); err != nil {
			http.Error(w, "Error scanning task", http.StatusInternalServerError)
			return
		}
		tasks = append(tasks, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}
