-- vod-service/migrations/001_create_recordings.up.sql
-- ✅ ИСПРАВЛЕНО: Делаем CREATE TABLE IF NOT EXISTS (таблица уже может существовать)
CREATE TABLE IF NOT EXISTS recordings (
    id SERIAL PRIMARY KEY,
    stream_id VARCHAR(100) UNIQUE NOT NULL,
    user_id INTEGER,
    title VARCHAR(255),
    description TEXT,
    duration_seconds INTEGER,
    file_path TEXT,
    thumbnail_path TEXT,
    file_size_bytes BIGINT,
    status VARCHAR(20) DEFAULT 'processing',
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Добавляем индексы только если их еще нет
CREATE INDEX IF NOT EXISTS idx_recordings_user_id ON recordings(user_id);
CREATE INDEX IF NOT EXISTS idx_recordings_status ON recordings(status);
CREATE INDEX IF NOT EXISTS idx_recordings_created_at ON recordings(created_at DESC);
