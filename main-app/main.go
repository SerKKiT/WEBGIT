package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"web/main-app/database"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

var (
	db         *pgx.Conn
	authClient *AuthClient // ✅ ДОБАВЛЕНО
)

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
	// Создаем Gorilla Mux router
	r := mux.NewRouter()

	// ===================================
	// LEGACY ENDPOINTS (БЕЗ АВТОРИЗАЦИИ) - для совместимости
	// ===================================
	r.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
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
	}).Methods("GET", "POST", "PUT", "DELETE")

	r.HandleFunc("/tasks/update_status_by_stream", UpdateTaskStatusByStreamHandler).Methods("PUT")
	r.HandleFunc("/tasks/active", GetActiveTasksHandler).Methods("GET")

	// ===================================
	// ПУБЛИЧНЫЕ ENDPOINTS (БЕЗ АВТОРИЗАЦИИ)
	// ===================================
	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/streams", PublicStreamsHandler).Methods("GET") // Список live стримов
	api.HandleFunc("/health", HealthHandler).Methods("GET")         // Health check

	// ===================================
	// АВТОРИЗОВАННЫЕ ENDPOINTS
	// ===================================
	protected := api.PathPrefix("/streams").Subrouter()

	// Middleware для всех защищенных endpoints
	protected.Use(func(next http.Handler) http.Handler {
		return authClient.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	})

	// Создание стримов - только streamer и admin
	protected.Handle("", authClient.RequireStreamerRole(CreateStreamHandler)).Methods("POST")

	// Управление своими стримами
	protected.HandleFunc("/{streamId}/start", StartStreamHandler).Methods("POST")
	protected.HandleFunc("/{streamId}/stop", StopStreamHandler).Methods("POST")

	// Список моих стримов
	protected.HandleFunc("/my", MyStreamsHandler).Methods("GET")

	// ===================================
	// DEBUG ENDPOINTS
	// ===================================
	r.HandleFunc("/debug/migrations", func(w http.ResponseWriter, r *http.Request) {
		if err := database.ShowMigrationStatus(db); err != nil {
			http.Error(w, fmt.Sprintf("Failed to get migration status: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Migration status logged to console"))
	}).Methods("GET")

	// Используем Gorilla Mux вместо стандартного http
	http.Handle("/", r)
}

func main() {
	log.Println("🚀 Starting Main-App with Auth integration...")

	// Подключение к базе данных
	var err error
	db, err = connectDB()
	if err != nil {
		log.Fatalf("❌ Unable to connect to database: %v", err)
	}
	defer db.Close(context.Background())

	// ✅ НОВОЕ: Инициализация Auth Client
	authClient = NewAuthClient()
	log.Println("✅ Auth client initialized successfully")

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
	log.Println("🌐 Main-app with Auth integration starting on :8080")
	log.Printf("📋 Available endpoints:")
	log.Printf("  LEGACY (no auth - for compatibility):")
	log.Printf("    GET/POST/PUT/DEL /tasks")
	log.Printf("    PUT  /tasks/update_status_by_stream")
	log.Printf("    GET  /tasks/active")
	log.Printf("  PUBLIC:")
	log.Printf("    GET  /api/health")
	log.Printf("    GET  /api/streams (live streams list)")
	log.Printf("  PROTECTED (require Bearer token):")
	log.Printf("    POST /api/streams (create stream - streamer/admin only)")
	log.Printf("    POST /api/streams/{id}/start")
	log.Printf("    POST /api/streams/{id}/stop")
	log.Printf("    GET  /api/streams/my")
	log.Printf("  AUTH SERVICE: %s", getEnv("AUTH_SERVICE_URL", "http://localhost:8082"))

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("❌ HTTP server failed: %v", err)
	}
}

// ✅ НОВАЯ ФУНКЦИЯ: Health check
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем подключение к БД
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var dbStatus string
	if err := db.Ping(ctx); err != nil {
		dbStatus = "disconnected"
	} else {
		dbStatus = "connected"
	}

	response := map[string]interface{}{
		"status":           "healthy",
		"service":          "main-app-with-auth",
		"version":          "2.0.0",
		"database":         dbStatus,
		"auth_integration": true,
		"timestamp":        time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
