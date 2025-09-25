-- Rollback миграции
DROP TRIGGER IF EXISTS update_recordings_updated_at ON recordings;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TABLE IF EXISTS recordings;
