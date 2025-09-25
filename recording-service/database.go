package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DatabaseManager struct {
	pool *pgxpool.Pool
}

func NewDatabaseManager() (*DatabaseManager, error) {
	// Формирование строки подключения из ENV переменных
	dbURL := buildDatabaseURL()

	log.Printf("📊 Connecting to database...")

	// ✅ АВТОМАТИЧЕСКИЕ МИГРАЦИИ
	if err := runMigrations(dbURL); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Создание connection pool
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Настройки pool
	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Тест подключения
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("✅ Database connected successfully")
	return &DatabaseManager{pool: pool}, nil
}

// Автоматическое применение миграций
// Автоматическое применение миграций с отладкой
func runMigrations(dbURL string) error {
	log.Println("🔄 Running database migrations...")

	// ✅ DEBUG: проверить рабочую директорию и файлы
	pwd, err := os.Getwd()
	if err != nil {
		log.Printf("⚠️ Could not get working directory: %v", err)
	} else {
		log.Printf("📁 Current working directory: %s", pwd)
	}

	// Проверить содержимое папки migrations
	migrationsPath := "./migrations"
	files, err := os.ReadDir(migrationsPath)
	if err != nil {
		log.Printf("❌ Cannot read migrations directory '%s': %v", migrationsPath, err)

		// Попробовать альтернативные пути
		altPaths := []string{"/app/migrations", "migrations", "../migrations"}
		for _, altPath := range altPaths {
			if altFiles, altErr := os.ReadDir(altPath); altErr == nil {
				log.Printf("✅ Found migrations in alternative path: %s", altPath)
				migrationsPath = altPath
				files = altFiles
				break
			}
		}

		if len(files) == 0 {
			return fmt.Errorf("migrations directory not found in any expected location")
		}
	}

	log.Printf("📂 Found %d files in migrations directory:", len(files))
	for _, file := range files {
		log.Printf("  - %s", file.Name())
	}

	// Проверить что есть .up.sql файлы
	hasUpFiles := false
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".up.sql") {
			hasUpFiles = true
			break
		}
	}

	if !hasUpFiles {
		return fmt.Errorf("no .up.sql migration files found in %s", migrationsPath)
	}

	// Использовать найденный путь для миграций
	sourceURL := fmt.Sprintf("file://%s", migrationsPath)
	log.Printf("🔧 Using migration source: %s", sourceURL)

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("failed to initialize migrations: %w", err)
	}
	defer m.Close()

	// Применить все миграции
	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			log.Println("📊 No new migrations to apply")
			return nil
		}
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	// Получить текущую версию
	version, dirty, err := m.Version()
	if err != nil {
		log.Printf("⚠️ Could not get migration version: %v", err)
	} else {
		log.Printf("✅ Database migrations completed (version: %d, dirty: %v)", version, dirty)
	}

	return nil
}

func buildDatabaseURL() string {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		return dbURL
	}

	host := getEnv("DB_HOST", "postgres")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "postgres")
	password := getEnv("DB_PASSWORD", "example")
	dbname := getEnv("DB_NAME", "appdb")

	// ✅ ДОБАВЛЕН sslmode=disable
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		user, password, host, port, dbname)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func (dm *DatabaseManager) UpdateRecordingStatus(streamID, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
        UPDATE recordings 
        SET status = $1, updated_at = NOW() 
        WHERE stream_id = $2`

	result, err := dm.pool.Exec(ctx, query, status, streamID)
	if err != nil {
		return fmt.Errorf("failed to update recording status: %w", err)
	}

	rowsAffected := result.RowsAffected()
	log.Printf("📊 DB: Updated %s status to %s (rows affected: %d)", streamID, status, rowsAffected)

	return nil
}

// Обновить функцию CreateRecording для поддержки username
func (dm *DatabaseManager) CreateRecording(recording Recording) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
        INSERT INTO recordings (
            stream_id, user_id, username, title, duration_seconds, 
            file_path, thumbnail_path, file_size_bytes, status, created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        ON CONFLICT (stream_id) DO UPDATE SET
            user_id = EXCLUDED.user_id,
            username = EXCLUDED.username,
            title = EXCLUDED.title,
            duration_seconds = EXCLUDED.duration_seconds,
            file_path = EXCLUDED.file_path,
            thumbnail_path = EXCLUDED.thumbnail_path,
            file_size_bytes = EXCLUDED.file_size_bytes,
            status = EXCLUDED.status,
            updated_at = NOW()`

	_, err := dm.pool.Exec(ctx, query,
		recording.StreamID,
		recording.UserID,   // ✅ СОХРАНЯЕМ USER_ID
		recording.Username, // ✅ СОХРАНЯЕМ USERNAME
		recording.Title,
		recording.Duration,
		recording.FilePath,
		recording.ThumbnailPath,
		recording.FileSize,
		recording.Status,
		recording.CreatedAt,
		recording.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create/update recording: %w", err)
	}

	log.Printf("📊 DB: Created/Updated recording for %s (owner: %s, user_id: %d, duration: %ds, size: %d bytes)",
		recording.StreamID, recording.Username, recording.UserID, recording.Duration, recording.FileSize)

	return nil
}

// ✅ НОВАЯ ФУНКЦИЯ: финальное обновление записи
func (dm *DatabaseManager) UpdateRecordingComplete(recording Recording) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
        UPDATE recordings SET
            user_id = $2,
            username = $3,
            title = $4,
            duration_seconds = $5,
            file_path = $6,
            thumbnail_path = $7,
            file_size_bytes = $8,
            status = $9,
            updated_at = NOW()
        WHERE stream_id = $1`

	result, err := dm.pool.Exec(ctx, query,
		recording.StreamID,
		recording.UserID,   // ✅ ОБНОВЛЯЕМ USER_ID
		recording.Username, // ✅ ОБНОВЛЯЕМ USERNAME
		recording.Title,
		recording.Duration,
		recording.FilePath,
		recording.ThumbnailPath,
		recording.FileSize,
		recording.Status,
	)

	if err != nil {
		return fmt.Errorf("failed to update recording complete: %w", err)
	}

	rowsAffected := result.RowsAffected()
	log.Printf("📊 DB: Updated recording complete for %s (owner: %s, rows affected: %d)",
		recording.StreamID, recording.Username, rowsAffected)

	return nil
}

// Обновить GetRecording для поддержки username
func (dm *DatabaseManager) GetRecording(streamID string) (*Recording, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
        SELECT id, stream_id, user_id, username, title, duration_seconds,
               file_path, thumbnail_path, file_size_bytes, status, created_at, updated_at
        FROM recordings 
        WHERE stream_id = $1`

	var r Recording
	err := dm.pool.QueryRow(ctx, query, streamID).Scan(
		&r.ID, &r.StreamID, &r.UserID, &r.Username, &r.Title, &r.Duration, // ✅ ДОБАВЛЕН USERNAME
		&r.FilePath, &r.ThumbnailPath, &r.FileSize, &r.Status,
		&r.CreatedAt, &r.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get recording: %w", err)
	}

	return &r, nil
}

// Обновить ListRecordings для поддержки username
func (dm *DatabaseManager) ListRecordings(limit, offset int) ([]Recording, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
        SELECT id, stream_id, user_id, username, title, duration_seconds,
               file_path, thumbnail_path, file_size_bytes, status, created_at, updated_at
        FROM recordings 
        ORDER BY created_at DESC 
        LIMIT $1 OFFSET $2`

	rows, err := dm.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list recordings: %w", err)
	}
	defer rows.Close()

	var recordings []Recording
	for rows.Next() {
		var r Recording
		err := rows.Scan(
			&r.ID, &r.StreamID, &r.UserID, &r.Username, &r.Title, &r.Duration, // ✅ ДОБАВЛЕН USERNAME
			&r.FilePath, &r.ThumbnailPath, &r.FileSize, &r.Status,
			&r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan recording: %w", err)
		}
		recordings = append(recordings, r)
	}

	log.Printf("📊 DB: Listed %d recordings (limit: %d, offset: %d)", len(recordings), limit, offset)
	return recordings, nil
}

// ✅ НОВАЯ ФУНКЦИЯ: получение записей пользователя
func (dm *DatabaseManager) GetUserRecordings(userID int, limit, offset int) ([]Recording, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
        SELECT id, stream_id, user_id, username, title, duration_seconds,
               file_path, thumbnail_path, file_size_bytes, status, created_at, updated_at
        FROM recordings 
        WHERE user_id = $1
        ORDER BY created_at DESC 
        LIMIT $2 OFFSET $3`

	rows, err := dm.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get user recordings: %w", err)
	}
	defer rows.Close()

	var recordings []Recording
	for rows.Next() {
		var r Recording
		err := rows.Scan(
			&r.ID, &r.StreamID, &r.UserID, &r.Username, &r.Title, &r.Duration,
			&r.FilePath, &r.ThumbnailPath, &r.FileSize, &r.Status,
			&r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user recording: %w", err)
		}
		recordings = append(recordings, r)
	}

	log.Printf("📊 DB: Listed %d recordings for user_id %d", len(recordings), userID)
	return recordings, nil
}

func (dm *DatabaseManager) Close() {
	if dm.pool != nil {
		dm.pool.Close()
		log.Println("📊 Database connection closed")
	}
}
