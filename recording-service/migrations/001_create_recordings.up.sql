CREATE TABLE IF NOT EXISTS recordings (
    id SERIAL PRIMARY KEY,
    stream_id VARCHAR(100) UNIQUE NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 0,           -- ✅ ОБЯЗАТЕЛЬНОЕ ПОЛЕ
    username VARCHAR(100) NOT NULL DEFAULT '',    -- ✅ НОВОЕ ПОЛЕ
    title VARCHAR(255) NOT NULL DEFAULT '',
    duration_seconds INTEGER DEFAULT 0,
    file_path TEXT DEFAULT '',                    -- MinIO VOD URL
    thumbnail_path TEXT DEFAULT '',               -- MinIO thumbnail URL  
    file_size_bytes BIGINT DEFAULT 0,
    status VARCHAR(20) DEFAULT 'processing',      -- processing/ready/failed
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Индексы для оптимизации
CREATE INDEX IF NOT EXISTS idx_recordings_stream_id ON recordings(stream_id);
CREATE INDEX IF NOT EXISTS idx_recordings_user_id ON recordings(user_id);     -- ✅ НОВЫЙ ИНДЕКС
CREATE INDEX IF NOT EXISTS idx_recordings_username ON recordings(username);   -- ✅ НОВЫЙ ИНДЕКС  
CREATE INDEX IF NOT EXISTS idx_recordings_status ON recordings(status);
CREATE INDEX IF NOT EXISTS idx_recordings_created_at ON recordings(created_at);

-- Комментарии
COMMENT ON TABLE recordings IS 'VOD recordings created from live streams';
COMMENT ON COLUMN recordings.user_id IS 'ID of user who created the stream';
COMMENT ON COLUMN recordings.username IS 'Username of stream owner';
COMMENT ON COLUMN recordings.file_path IS 'MinIO path to MP4 file';
COMMENT ON COLUMN recordings.thumbnail_path IS 'MinIO path to thumbnail';
