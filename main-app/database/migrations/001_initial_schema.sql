-- Migration: Initial database schema
-- Description: Create ENUM types and initial tables

-- +migrate Up

-- Создаем ENUM тип task_status, если его нет
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'task_status') THEN
        CREATE TYPE task_status AS ENUM ('stopped', 'waiting', 'running', 'error');
    END IF;
END $$;

-- Создаем таблицу messages
CREATE TABLE IF NOT EXISTS messages (
    id SERIAL PRIMARY KEY,
    text TEXT NOT NULL
);

-- Создаем таблицу Tasks с использованием типа task_status
CREATE TABLE IF NOT EXISTS Tasks (
    ID SERIAL PRIMARY KEY,
    StreamID TEXT UNIQUE NOT NULL,
    Name TEXT NOT NULL,
    Created TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    Updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    Status task_status NOT NULL
);

-- Добавляем комментарии для документации
COMMENT ON TABLE Tasks IS 'Streaming tasks management table';
COMMENT ON COLUMN Tasks.StreamID IS 'Unique identifier for stream';
COMMENT ON COLUMN Tasks.Status IS 'Current status of the streaming task';

-- +migrate Down

-- Удаляем таблицы в обратном порядке
DROP TABLE IF EXISTS Tasks;
DROP TABLE IF EXISTS messages;

-- Удаляем ENUM тип (осторожно в production!)
DROP TYPE IF EXISTS task_status;
