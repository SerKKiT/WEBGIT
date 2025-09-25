package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5" // ✅ ИСПРАВЛЕНО: pgx вместо database/sql
)

func (h *Handlers) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) respondError(w http.ResponseWriter, err APIError) {
	h.respondJSON(w, err.Code, map[string]interface{}{
		"error":   err.Message,
		"details": err.Details,
		"code":    err.Code,
	})
}

func (h *Handlers) handleError(w http.ResponseWriter, err error) {
	if apiErr, ok := err.(APIError); ok {
		h.respondError(w, apiErr)
		return
	}
	h.respondError(w, NewInternalError(err.Error()))
}

func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":  "healthy",
		"service": "vod-service",
		"time":    time.Now().UTC(),
	}

	// Проверка соединения с базой данных
	if err := h.db.Ping(r.Context()); err != nil {
		status["status"] = "unhealthy"
		status["database"] = "disconnected"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		status["database"] = "connected"
	}

	h.respondJSON(w, http.StatusOK, status)
}

// ✅ ИСПРАВЛЕНО: Упрощенная версия ListRecordings
func (h *Handlers) ListRecordings(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")

	if page < 0 {
		page = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	// ✅ ИСПРАВЛЕНО: Прямой SQL запрос вместо вызова несуществующей функции
	query := `
        SELECT id, stream_id, user_id, title, duration_seconds, file_path, 
               thumbnail_path, file_size_bytes, status, created_at, updated_at
        FROM recordings
        WHERE 1=1
    `
	args := []interface{}{}
	argCount := 0

	// Фильтрация по статусу
	if status != "" {
		argCount++
		query += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, status)
	}

	// Поиск по названию
	if search != "" {
		argCount++
		query += fmt.Sprintf(" AND title ILIKE $%d", argCount)
		args = append(args, "%"+search+"%")
	}

	query += " ORDER BY created_at DESC"

	// LIMIT и OFFSET
	argCount++
	query += fmt.Sprintf(" LIMIT $%d", argCount)
	args = append(args, limit+1) // +1 для проверки hasMore

	argCount++
	query += fmt.Sprintf(" OFFSET $%d", argCount)
	args = append(args, page*limit)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		h.handleError(w, NewInternalError("Database query failed: "+err.Error()))
		return
	}
	defer rows.Close()

	var recordings []RecordingResponse
	for rows.Next() {
		var r Recording
		err := rows.Scan(
			&r.ID, &r.StreamID, &r.UserID, &r.Title, &r.Duration,
			&r.FilePath, &r.ThumbnailPath, &r.FileSize, &r.Status,
			&r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			h.handleError(w, NewInternalError("Row scan failed: "+err.Error()))
			return
		}

		recording := RecordingResponse{Recording: r}
		recordings = append(recordings, recording)
	}

	hasMore := len(recordings) > limit
	if hasMore {
		recordings = recordings[:limit]
	}

	response := ListRecordingsResponse{
		Recordings: recordings,
		HasMore:    hasMore,
		Page:       page,
		Limit:      limit,
	}

	h.respondJSON(w, http.StatusOK, response)
}

// ✅ ИСПРАВЛЕНО: Упрощенная версия GetRecording
func (h *Handlers) GetRecording(w http.ResponseWriter, r *http.Request) {
	streamID := mux.Vars(r)["streamId"]

	var rec Recording
	query := `
        SELECT id, stream_id, user_id, title, duration_seconds, file_path,
               thumbnail_path, file_size_bytes, status, created_at, updated_at
        FROM recordings
        WHERE stream_id = $1
    `

	err := h.db.QueryRow(r.Context(), query, streamID).Scan(
		&rec.ID, &rec.StreamID, &rec.UserID, &rec.Title, &rec.Duration,
		&rec.FilePath, &rec.ThumbnailPath, &rec.FileSize, &rec.Status,
		&rec.CreatedAt, &rec.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows { // ✅ ИСПРАВЛЕНО: pgx.ErrNoRows вместо sql.ErrNoRows
			h.respondError(w, NewNotFoundError("Recording", streamID))
			return
		}
		h.handleError(w, NewInternalError("Database query failed: "+err.Error()))
		return
	}

	response := RecordingResponse{Recording: rec}
	h.respondJSON(w, http.StatusOK, response)
}

func (h *Handlers) GetStreamURL(w http.ResponseWriter, r *http.Request) {
	streamID := mux.Vars(r)["streamId"]

	var status string
	err := h.db.QueryRow(r.Context(), "SELECT status FROM recordings WHERE stream_id = $1", streamID).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows { // ✅ ИСПРАВЛЕНО: pgx.ErrNoRows
			h.respondError(w, NewNotFoundError("Recording", streamID))
			return
		}
		h.handleError(w, NewInternalError("Database query failed"))
		return
	}

	if status != "ready" {
		h.respondError(w, NewValidationError(fmt.Sprintf("Recording is not ready (status: %s)", status)))
		return
	}

	expiresIn := 3600
	if expiresStr := r.URL.Query().Get("expires_in"); expiresStr != "" {
		if exp, err := strconv.Atoi(expiresStr); err == nil && exp >= 300 && exp <= 86400 {
			expiresIn = exp
		}
	}

	streamURL, err := h.storage.GetPresignedStreamURL(r.Context(), streamID, time.Duration(expiresIn)*time.Second)
	if err != nil {
		h.handleError(w, NewInternalError("Failed to generate stream URL"))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"stream_url": streamURL,
		"expires_at": time.Now().Add(time.Duration(expiresIn) * time.Second),
		"expires_in": expiresIn,
	})
}

func (h *Handlers) GetThumbnailURL(w http.ResponseWriter, r *http.Request) {
	streamID := mux.Vars(r)["streamId"]

	var status string
	err := h.db.QueryRow(r.Context(), "SELECT status FROM recordings WHERE stream_id = $1", streamID).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows { // ✅ ИСПРАВЛЕНО: pgx.ErrNoRows
			h.respondError(w, NewNotFoundError("Recording", streamID))
			return
		}
		h.handleError(w, NewInternalError("Database query failed"))
		return
	}

	if status != "ready" {
		h.respondError(w, NewValidationError(fmt.Sprintf("Recording is not ready (status: %s)", status)))
		return
	}

	expiresIn := 3600
	if expiresStr := r.URL.Query().Get("expires_in"); expiresStr != "" {
		if exp, err := strconv.Atoi(expiresStr); err == nil && exp >= 300 && exp <= 86400 {
			expiresIn = exp
		}
	}

	thumbnailURL, err := h.storage.GetPresignedThumbnailURL(r.Context(), streamID, time.Duration(expiresIn)*time.Second)
	if err != nil {
		h.handleError(w, NewInternalError("Failed to generate thumbnail URL"))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"thumbnail_url": thumbnailURL,
		"expires_at":    time.Now().Add(time.Duration(expiresIn) * time.Second),
		"expires_in":    expiresIn,
	})
}

// ✅ ОБНОВЛЕННЫЙ UpdateRecording с проверкой владельца
func (h *Handlers) UpdateRecording(w http.ResponseWriter, r *http.Request) {
	streamID := mux.Vars(r)["streamId"]
	claims, ok := r.Context().Value("user").(*AuthClaims)
	if !ok {
		h.respondError(w, NewInternalError("User context not found"))
		return
	}

	// Проверяем, может ли пользователь редактировать эту запись
	if !h.canModifyRecording(r.Context(), streamID, claims) {
		h.respondError(w, APIError{
			Code:    403,
			Message: "Forbidden",
			Details: "You can only modify your own recordings (unless you're an admin)",
		})
		return
	}

	var req UpdateRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, NewValidationError("Invalid JSON body"))
		return
	}

	if req.Title == nil {
		h.respondError(w, NewValidationError("No fields to update"))
		return
	}

	query := `UPDATE recordings SET title = $1, updated_at = NOW() WHERE stream_id = $2`
	result, err := h.db.Exec(r.Context(), query, *req.Title, streamID)
	if err != nil {
		h.handleError(w, NewInternalError("Database update failed"))
		return
	}

	if result.RowsAffected() == 0 {
		h.respondError(w, NewNotFoundError("Recording", streamID))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":    "Recording updated successfully",
		"updated_by": claims.Username,
		"role":       claims.Role,
	})
}

// ✅ ОБНОВЛЕННЫЙ DeleteRecording с проверкой владельца
func (h *Handlers) DeleteRecording(w http.ResponseWriter, r *http.Request) {
	streamID := mux.Vars(r)["streamId"]
	claims, ok := r.Context().Value("user").(*AuthClaims)
	if !ok {
		h.respondError(w, NewInternalError("User context not found"))
		return
	}

	// Проверяем, может ли пользователь удалить эту запись
	if !h.canModifyRecording(r.Context(), streamID, claims) {
		h.respondError(w, APIError{
			Code:    403,
			Message: "Forbidden",
			Details: "You can only delete your own recordings (unless you're an admin)",
		})
		return
	}

	result, err := h.db.Exec(r.Context(), "DELETE FROM recordings WHERE stream_id = $1", streamID)
	if err != nil {
		h.handleError(w, NewInternalError("Database delete failed"))
		return
	}

	if result.RowsAffected() == 0 {
		h.respondError(w, NewNotFoundError("Recording", streamID))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":    "Recording deleted successfully",
		"deleted_by": claims.Username,
		"role":       claims.Role,
	})
}

// ✅ НОВАЯ ФУНКЦИЯ: проверка прав на модификацию записи
func (h *Handlers) canModifyRecording(ctx context.Context, streamID string, claims *AuthClaims) bool {
	// Admin может модифицировать любые записи
	if claims.Role == "admin" {
		return true
	}

	// Для остальных - проверяем владельца записи
	var ownerID int
	query := `SELECT user_id FROM recordings WHERE stream_id = $1`
	err := h.db.QueryRow(ctx, query, streamID).Scan(&ownerID)
	if err != nil {
		return false // Запись не найдена
	}

	// Пользователь может модифицировать только свои записи
	return ownerID == claims.UserID
}
