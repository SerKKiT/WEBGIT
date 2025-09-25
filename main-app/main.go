package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"web/main-app/database"

	"github.com/jackc/pgx/v5"
)

var db *pgx.Conn

func connectDB() (*pgx.Conn, error) {
	cfg := LoadDBConfig()
	var conn *pgx.Conn
	var err error

	log.Printf("Connecting to database: %s", cfg.Host)

	for i := 0; i < 10; i++ {
		conn, err = pgx.Connect(context.Background(), cfg.DSN())
		if err == nil {
			log.Println("✅ Database connection established")
			return conn, nil
		}
		log.Printf("⏳ DB not ready, retrying in 2 seconds... (attempt %d/10)", i+1)
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("failed to connect after 10 attempts: %v", err)
}

func runMigrations() error {
	log.Println("🔄 Starting database migrations...")

	// Инициализируем таблицу миграций
	if err := database.InitMigrationTable(db); err != nil {
		return fmt.Errorf("failed to initialize migration table: %v", err)
	}

	// Загружаем файлы миграций
	migrations, err := database.LoadMigrations("./database/migrations")
	if err != nil {
		return fmt.Errorf("failed to load migrations: %v", err)
	}

	if len(migrations) == 0 {
		log.Println("⚠️  No migration files found in ./database/migrations")
		return nil
	}

	log.Printf("📁 Found %d migration files", len(migrations))

	// Применяем миграции
	if err := database.ApplyMigrations(db, migrations); err != nil {
		return fmt.Errorf("failed to apply migrations: %v", err)
	}

	// Показываем статус миграций
	if err := database.ShowMigrationStatus(db); err != nil {
		log.Printf("Warning: failed to show migration status: %v", err)
	}

	log.Println("✅ Database migrations completed successfully")
	return nil
}

func setupRoutes() {
	http.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			GetTasksHandler(w, r)
		case http.MethodPost:
			CreateTaskHandler(w, r)
		case http.MethodPut:
			UpdateTaskHandler(w, r)
		case http.MethodDelete:
			DeleteTaskHandler(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/tasks/update_status_by_stream", UpdateTaskStatusByStreamHandler)
	http.HandleFunc("/tasks/active", GetActiveTasksHandler)

	// Добавляем endpoint для проверки миграций
	http.HandleFunc("/debug/migrations", func(w http.ResponseWriter, r *http.Request) {
		if err := database.ShowMigrationStatus(db); err != nil {
			http.Error(w, fmt.Sprintf("Failed to get migration status: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Migration status logged to console"))
	})
}

func main() {
	log.Println("🚀 Starting Main-App...")

	// Подключение к базе данных
	var err error
	db, err = connectDB()
	if err != nil {
		log.Fatalf("❌ Unable to connect to database: %v", err)
	}
	defer db.Close(context.Background())

	// Проверяем флаг для выполнения только миграций
	if len(os.Args) > 1 && os.Args[1] == "--migrate-only" {
		log.Println("🔧 Running migrations only...")
		if err := runMigrations(); err != nil {
			log.Fatalf("❌ Migration failed: %v", err)
		}
		log.Println("✅ Migrations completed, exiting")
		return
	}

	// Выполняем миграции
	if err := runMigrations(); err != nil {
		log.Fatalf("❌ Failed to run migrations: %v", err)
	}

	// Настраиваем маршруты
	setupRoutes()

	// Запускаем сервер
	log.Println("🌐 Main-app server starting on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("❌ HTTP server failed: %v", err)
	}
}
