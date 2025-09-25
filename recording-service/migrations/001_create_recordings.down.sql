-- Удаление индексов
DROP INDEX IF EXISTS idx_recordings_created_at;
DROP INDEX IF EXISTS idx_recordings_status; 
DROP INDEX IF EXISTS idx_recordings_username;
DROP INDEX IF EXISTS idx_recordings_user_id;
DROP INDEX IF EXISTS idx_recordings_stream_id;

-- Удаление таблицы
DROP TABLE IF EXISTS recordings;
