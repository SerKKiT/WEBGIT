-- +migrate Up
-- Добавляем поля для авторизации в таблицу Tasks

ALTER TABLE Tasks ADD COLUMN user_id INTEGER;
ALTER TABLE Tasks ADD COLUMN username VARCHAR(100);

-- Создаем индексы для оптимизации запросов
CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON Tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_tasks_username ON Tasks(username);

-- Обновляем существующие записи (опционально, для legacy задач)
UPDATE Tasks SET user_id = 0, username = 'system' WHERE user_id IS NULL;

-- +migrate Down
-- Откат изменений

DROP INDEX IF EXISTS idx_tasks_username;
DROP INDEX IF EXISTS idx_tasks_user_id;
ALTER TABLE Tasks DROP COLUMN IF EXISTS username;
ALTER TABLE Tasks DROP COLUMN IF EXISTS user_id;
