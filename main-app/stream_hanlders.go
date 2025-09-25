package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

// StreamRequest структура для создания стрима
type StreamRequest struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
}

// StreamResponse структура ответа при создании стрима
type StreamResponse struct {
	ID          int       `json:"id"`
	StreamID    string    `json:"stream_id"`
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	UserID      int       `json:"user_id"`
	Username    string    `json:"username"`
	Status      string    `json:"status"`
	Created     time.Time `json:"created"`
	SRTEndpoint string    `json:"srt_endpoint,omitempty"`
	HLSUrl      string    `json:"hls_url,omitempty"`
}

// CreateStreamHandler создает новый стрим (авторизованный)
func CreateStreamHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value("user").(*AuthClaims)
	if !ok {
		http.Error(w, "User context not found", http.StatusInternalServerError)
		return
	}

	var req StreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Stream name is required", http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		req.Title = fmt.Sprintf("%s's Stream", claims.Username)
	}

	// Генерируем StreamID
	streamID, err := generateStreamID()
	if err != nil {
		http.Error(w, "Failed to generate stream ID", http.StatusInternalServerError)
		return
	}

	// Создаем задачу в БД с информацией о пользователе
	var task Task
	err = db.QueryRow(context.Background(),
		`INSERT INTO Tasks (streamid, name, user_id, username, status) 
         VALUES ($1, $2, $3, $4, $5) 
         RETURNING id, created, updated`,
		streamID, req.Title, claims.UserID, claims.Username, "stopped").
		Scan(&task.ID, &task.Created, &task.Updated)

	if err != nil {
		log.Printf("Failed to create stream task: %v", err)
		http.Error(w, "Failed to create stream", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Stream created: %s by %s (ID: %d, role: %s)", streamID, claims.Username, claims.UserID, claims.Role)

	// Формируем ответ
	response := StreamResponse{
		ID:       task.ID,
		StreamID: streamID,
		Name:     req.Name,
		Title:    req.Title,
		UserID:   claims.UserID,
		Username: claims.Username,
		Status:   "stopped",
		Created:  task.Created,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// StartStreamHandler запускает стрим (авторизованный)
func StartStreamHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value("user").(*AuthClaims)
	if !ok {
		http.Error(w, "User context not found", http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)
	streamID := vars["streamId"]

	if streamID == "" {
		http.Error(w, "Stream ID is required", http.StatusBadRequest)
		return
	}

	// Получаем информацию о стриме из БД
	var task Task
	err := db.QueryRow(context.Background(),
		`SELECT id, streamid, name, user_id, username, status FROM Tasks WHERE streamid = $1`,
		streamID).Scan(&task.ID, &task.StreamID, &task.Name, &task.UserID, &task.Username, &task.Status)

	if err != nil {
		http.Error(w, "Stream not found", http.StatusNotFound)
		return
	}

	// Проверка прав доступа
	if task.UserID != claims.UserID && claims.Role != "admin" {
		http.Error(w, "You can only start your own streams", http.StatusForbidden)
		return
	}

	if task.Status != "stopped" {
		http.Error(w, fmt.Sprintf("Stream cannot be started. Current status: %s", task.Status), http.StatusBadRequest)
		return
	}

	// Обновляем статус в БД
	_, err = db.Exec(context.Background(),
		`UPDATE Tasks SET status = 'waiting', updated = NOW() WHERE id = $1`,
		task.ID)

	if err != nil {
		log.Printf("Failed to update stream status: %v", err)
		http.Error(w, "Failed to start stream", http.StatusInternalServerError)
		return
	}

	// ✅ УВЕДОМЛЯЕМ STREAM-APP С ИНФОРМАЦИЕЙ О ПОЛЬЗОВАТЕЛЕ
	if err := notifyStreamAppWithUserInfo(streamID, "waiting", task.ID, claims.UserID, claims.Username, task.Name); err != nil {
		log.Printf("Failed to notify stream-app: %v", err)
		// Откатываем статус
		db.Exec(context.Background(), `UPDATE Tasks SET status = 'stopped' WHERE id = $1`, task.ID)
		http.Error(w, "Failed to start streaming process", http.StatusInternalServerError)
		return
	}

	log.Printf("🔴 Stream started: %s by %s (ID: %d)", streamID, claims.Username, claims.UserID)

	response := StreamResponse{
		ID:          task.ID,
		StreamID:    streamID,
		Name:        task.Name,
		Title:       task.Name,
		UserID:      claims.UserID,
		Username:    claims.Username,
		Status:      "waiting",
		SRTEndpoint: fmt.Sprintf("srt://localhost:10000?streamid=%s", streamID),
		HLSUrl:      fmt.Sprintf("http://localhost:9090/hls/%s/stream.m3u8", streamID),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// StopStreamHandler останавливает стрим (авторизованный)
func StopStreamHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value("user").(*AuthClaims)
	if !ok {
		http.Error(w, "User context not found", http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)
	streamID := vars["streamId"]

	if streamID == "" {
		http.Error(w, "Stream ID is required", http.StatusBadRequest)
		return
	}

	// Проверяем, что стрим существует и принадлежит пользователю (или пользователь - админ)
	var task Task
	err := db.QueryRow(context.Background(),
		`SELECT id, streamid, name, user_id, username, status FROM Tasks WHERE streamid = $1`,
		streamID).Scan(&task.ID, &task.StreamID, &task.Name, &task.UserID, &task.Username, &task.Status)

	if err != nil {
		http.Error(w, "Stream not found", http.StatusNotFound)
		return
	}

	// Проверка прав доступа
	if task.UserID != claims.UserID && claims.Role != "admin" {
		http.Error(w, "You can only stop your own streams", http.StatusForbidden)
		return
	}

	if task.Status == "stopped" {
		http.Error(w, "Stream is already stopped", http.StatusBadRequest)
		return
	}

	// Обновляем статус в БД
	_, err = db.Exec(context.Background(),
		`UPDATE Tasks SET status = 'stopped', updated = NOW() WHERE id = $1`,
		task.ID)

	if err != nil {
		log.Printf("Failed to update stream status: %v", err)
		http.Error(w, "Failed to stop stream", http.StatusInternalServerError)
		return
	}

	// Уведомляем stream-app об остановке
	if err := notifyStreamApp(streamID, "stopped", task.ID); err != nil {
		log.Printf("Failed to notify stream-app about stop: %v", err)
		// Не возвращаем ошибку, так как статус в БД уже обновлен
	}

	log.Printf("⏹️ Stream stopped: %s by %s (ID: %d)", streamID, claims.Username, claims.UserID)

	response := map[string]interface{}{
		"stream_id": streamID,
		"status":    "stopped",
		"user_id":   claims.UserID,
		"username":  claims.Username,
		"message":   "Stream stopped successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// MyStreamsHandler показывает стримы пользователя
func MyStreamsHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value("user").(*AuthClaims)
	if !ok {
		http.Error(w, "User context not found", http.StatusInternalServerError)
		return
	}

	// Для админа - все стримы, для остальных - только свои
	var rows pgx.Rows
	var err error

	if claims.Role == "admin" {
		rows, err = db.Query(context.Background(),
			`SELECT id, streamid, name, user_id, username, created, updated, status 
             FROM Tasks ORDER BY created DESC`)
	} else {
		rows, err = db.Query(context.Background(),
			`SELECT id, streamid, name, user_id, username, created, updated, status 
             FROM Tasks WHERE user_id = $1 ORDER BY created DESC`,
			claims.UserID)
	}

	if err != nil {
		http.Error(w, "Failed to fetch streams", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var streams []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.StreamID, &t.Name, &t.UserID, &t.Username, &t.Created, &t.Updated, &t.Status); err != nil {
			http.Error(w, "Error scanning stream", http.StatusInternalServerError)
			return
		}
		streams = append(streams, t)
	}

	response := map[string]interface{}{
		"streams":  streams,
		"count":    len(streams),
		"user_id":  claims.UserID,
		"username": claims.Username,
		"role":     claims.Role,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// PublicStreamsHandler показывает все активные стримы (публично)
func PublicStreamsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(context.Background(),
		`SELECT id, streamid, name, user_id, username, created, status 
         FROM Tasks WHERE status IN ('waiting', 'running') 
         ORDER BY created DESC`)

	if err != nil {
		http.Error(w, "Failed to fetch public streams", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var streams []map[string]interface{}
	for rows.Next() {
		var id, userID int
		var streamID, name, username, status string
		var created time.Time

		if err := rows.Scan(&id, &streamID, &name, &userID, &username, &created, &status); err != nil {
			http.Error(w, "Error scanning stream", http.StatusInternalServerError)
			return
		}

		stream := map[string]interface{}{
			"stream_id": streamID,
			"title":     name,
			"username":  username,
			"status":    status,
			"created":   created,
			"hls_url":   fmt.Sprintf("http://localhost:9090/hls/%s/stream.m3u8", streamID),
		}

		streams = append(streams, stream)
	}

	response := map[string]interface{}{
		"live_streams": streams,
		"count":        len(streams),
		"endpoint":     "public",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
