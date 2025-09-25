package main

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Handlers структура для обработчиков
type Handlers struct {
	db      *pgxpool.Pool
	storage *Storage
}

type Recording struct {
	ID            int       `json:"id" db:"id"`
	StreamID      string    `json:"stream_id" db:"stream_id"`
	UserID        *int      `json:"user_id,omitempty" db:"user_id"`
	Title         *string   `json:"title,omitempty" db:"title"`
	Duration      *int      `json:"duration_seconds,omitempty" db:"duration_seconds"`
	FilePath      *string   `json:"file_path,omitempty" db:"file_path"`
	ThumbnailPath *string   `json:"thumbnail_path,omitempty" db:"thumbnail_path"`
	FileSize      *int64    `json:"file_size_bytes,omitempty" db:"file_size_bytes"`
	Status        string    `json:"status" db:"status"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

type RecordingResponse struct {
	Recording
	StreamURL    *string `json:"stream_url,omitempty"`
	ThumbnailURL *string `json:"thumbnail_url,omitempty"`
}

type ListRecordingsRequest struct {
	Page   int     `json:"page"`
	Limit  int     `json:"limit"`
	Status *string `json:"status,omitempty"`
	UserID *int    `json:"user_id,omitempty"`
	Search *string `json:"search,omitempty"`
}

type ListRecordingsResponse struct {
	Recordings []RecordingResponse `json:"recordings"`
	HasMore    bool                `json:"has_more"`
	Page       int                 `json:"page"`
	Limit      int                 `json:"limit"`
}

type UpdateRecordingRequest struct {
	Title *string `json:"title,omitempty"`
}

// API Error types
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

func (e APIError) Error() string {
	return e.Message
}

func NewValidationError(message string) APIError {
	return APIError{Code: 400, Message: message}
}

func NewNotFoundError(resource, id string) APIError {
	return APIError{Code: 404, Message: resource + " not found", Details: "ID: " + id}
}

func NewInternalError(message string) APIError {
	return APIError{Code: 500, Message: "Internal server error", Details: message}
}
