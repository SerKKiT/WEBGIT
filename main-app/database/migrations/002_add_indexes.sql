-- Migration: Add performance indexes
-- Description: Add indexes for common query patterns

-- +migrate Up

-- Индекс для быстрого поиска по StreamID
CREATE INDEX IF NOT EXISTS idx_tasks_streamid ON Tasks(StreamID);

-- Индекс для фильтрации по статусу  
CREATE INDEX IF NOT EXISTS idx_tasks_status ON Tasks(Status);

-- Индекс для сортировки по времени создания
CREATE INDEX IF NOT EXISTS idx_tasks_created ON Tasks(Created DESC);

-- Композитный индекс для запросов активных задач
CREATE INDEX IF NOT EXISTS idx_tasks_status_updated ON Tasks(Status, Updated DESC);

-- Partial index только для активных стримов
CREATE INDEX IF NOT EXISTS idx_tasks_active_streams 
ON Tasks(StreamID, Updated DESC) 
WHERE Status IN ('waiting', 'running');

-- Логирование успешного создания
DO $$
BEGIN
    RAISE NOTICE 'Performance indexes created successfully for Tasks table';
END $$;

-- +migrate Down

-- Удаляем индексы в обратном порядке
DROP INDEX IF EXISTS idx_tasks_active_streams;
DROP INDEX IF EXISTS idx_tasks_status_updated;
DROP INDEX IF EXISTS idx_tasks_created;
DROP INDEX IF EXISTS idx_tasks_status;
DROP INDEX IF EXISTS idx_tasks_streamid;
