-- Простая и надежная схема для пользователей
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(20) DEFAULT 'viewer' NOT NULL,
    is_active BOOLEAN DEFAULT true NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
    
    -- Ограничения для ролей
    CONSTRAINT users_role_check CHECK (role IN ('admin', 'streamer', 'viewer')),
    CONSTRAINT users_email_check CHECK (email ~* '^[A-Za-z0-9._%-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'),
    CONSTRAINT users_username_check CHECK (LENGTH(username) >= 3)
);

-- Простые и необходимые индексы
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
CREATE INDEX IF NOT EXISTS idx_users_active ON users(is_active);

-- Функция для обновления updated_at (создаем с OR REPLACE)
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- ✅ ИСПРАВЛЕНИЕ: Удаляем триггер если существует, затем создаем
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
CREATE TRIGGER update_users_updated_at 
    BEFORE UPDATE ON users 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();

-- Тестовые пользователи для каждой роли
INSERT INTO users (username, email, password_hash, role, is_active) VALUES
-- ✅ ИСПРАВЛЕННЫЕ ХЕШИ ПАРОЛЕЙ
-- Администратор (пароль: admin123)
('admin', 'admin@localhost.dev', '$2a$10$N9qo8uLOickgx2ZMRZoMye.J9FkCO0.8Z.bHL5QWoZe8MdePaU4C6', 'admin', true),

-- Стример (пароль: streamer123) 
('streamer1', 'streamer@localhost.dev', '$2a$10$5K8pMEV6DL2jlbr5t/y3E.QK5McnIDY.3U1CYq1ZZLfBCN3cPv3cS', 'streamer', true),

-- Зритель (пароль: viewer123)
('viewer1', 'viewer@localhost.dev', '$2a$10$1qPzXUjh3hYU9iAl7.VaZ.xZhL9xXV0WGLXdNpUIzMO1LdwxsQIVC', 'viewer', true),

-- ✅ ДОБАВИМ ТЕСТОВОГО СТРИМЕРА ДЛЯ НОВЫХ ТЕСТОВ
('newstreamer', 'newstreamer@test.local', '$2a$10$YgK2fCfm3QJ9gQfJMdPJEOlCUV8j3YtQCfgJDQhJPRQ8fYzCKxZJa', 'streamer', true)

ON CONFLICT (email) DO NOTHING;
