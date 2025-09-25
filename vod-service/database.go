package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ConnectDB() (*pgxpool.Pool, error) {
	// ✅ ИСПРАВЛЕНО: Сначала пробуем DATABASE_URL, если есть
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		log.Printf("Using DATABASE_URL for connection")

		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse DATABASE_URL: %w", err)
		}

		// Настройка пула соединений
		config.MaxConns = 25
		config.MinConns = 5
		config.MaxConnLifetime = time.Hour
		config.MaxConnIdleTime = time.Minute * 30

		db, err := pgxpool.NewWithConfig(context.Background(), config)
		if err != nil {
			return nil, fmt.Errorf("failed to create connection pool: %w", err)
		}

		// Проверка соединения
		if err := db.Ping(context.Background()); err != nil {
			return nil, fmt.Errorf("failed to ping database: %w", err)
		}

		log.Println("✅ Connected to database via DATABASE_URL")
		return db, nil
	}

	// ✅ FALLBACK: Если нет DATABASE_URL, используем отдельные переменные
	log.Printf("DATABASE_URL not found, using individual DB_* variables")

	dbHost := getEnv("DB_HOST", "recording-postgres") // ✅ ИСПРАВЛЕНО: recording-postgres по умолчанию
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "example") // ✅ ИСПРАВЛЕНО: example по умолчанию
	dbName := getEnv("DB_NAME", "recording_db")    // ✅ ИСПРАВЛЕНО: recording_db по умолчанию

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	log.Printf("Connecting to: host=%s port=%s dbname=%s user=%s", dbHost, dbPort, dbName, dbUser)

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Настройка пула соединений
	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = time.Minute * 30

	db, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Проверка соединения
	if err := db.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("✅ Connected to database: %s/%s", dbHost, dbName)

	// Создание таблицы если её нет
	if err := createTablesIfNotExist(db); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return db, nil
}

func createTablesIfNotExist(db *pgxpool.Pool) error {
	query := `
    CREATE TABLE IF NOT EXISTS recordings (
        id SERIAL PRIMARY KEY,
        stream_id VARCHAR(255) UNIQUE NOT NULL,
        user_id INTEGER,
        title TEXT,
        duration_seconds INTEGER,
        file_path TEXT,
        thumbnail_path TEXT,
        file_size_bytes BIGINT,
        status VARCHAR(50) DEFAULT 'processing',
        created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
        updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
    );

    CREATE INDEX IF NOT EXISTS idx_recordings_stream_id ON recordings(stream_id);
    CREATE INDEX IF NOT EXISTS idx_recordings_user_id ON recordings(user_id);
    CREATE INDEX IF NOT EXISTS idx_recordings_status ON recordings(status);
    CREATE INDEX IF NOT EXISTS idx_recordings_created_at ON recordings(created_at);
    `

	_, err := db.Exec(context.Background(), query)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	log.Println("✅ Database tables created/verified")
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
