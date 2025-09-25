package main

import "time"

// RecordingTask структура задачи из Kafka
type RecordingTask struct {
	StreamID     string    `json:"stream_id"`
	UserID       int       `json:"user_id,omitempty"`
	Title        string    `json:"title"`
	Action       string    `json:"action"`
	HLSPath      string    `json:"hls_path"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Duration     int       `json:"duration_seconds"`
	FileSize     int64     `json:"file_size_bytes,omitempty"`
	SegmentCount int       `json:"segment_count,omitempty"`
	Status       string    `json:"status"`
	Timestamp    time.Time `json:"timestamp"`
}

// Recording структура записи в БД
type Recording struct {
	ID            int       `json:"id"`
	StreamID      string    `json:"stream_id"`
	UserID        int       `json:"user_id"`
	Title         string    `json:"title"`
	Duration      int       `json:"duration_seconds"`
	FilePath      string    `json:"file_path"`
	ThumbnailPath string    `json:"thumbnail_path"`
	FileSize      int64     `json:"file_size_bytes"`
	Status        string    `json:"status"` // processing, ready, failed
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ProcessingResult результат обработки
type ProcessingResult struct {
	Success       bool
	MP4Path       string
	ThumbnailPath string
	FileSize      int64
	Error         error
}
