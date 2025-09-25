-- Создание таблицы для записей
CREATE TABLE recordings (
    id SERIAL PRIMARY KEY,
    stream_id VARCHAR(100) UNIQUE NOT NULL,
    user_id INTEGER,
    title VARCHAR(255),
    duration_seconds INTEGER,
    file_path TEXT,
    thumbnail_path TEXT,
    file_size_bytes BIGINT,
    status VARCHAR(20) DEFAULT 'processing',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Индексы для оптимизации запросов
CREATE INDEX idx_recordings_stream_id ON recordings(stream_id);
CREATE INDEX idx_recordings_status ON recordings(status);
CREATE INDEX idx_recordings_created_at ON recordings(created_at DESC);
CREATE INDEX idx_recordings_user_id ON recordings(user_id);

-- Функция автообновления updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Триггер для автообновления updated_at
CREATE TRIGGER update_recordings_updated_at 
    BEFORE UPDATE ON recordings 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();
