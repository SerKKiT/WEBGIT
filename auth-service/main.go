package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"web/auth-service/handlers"
	"web/auth-service/models"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

func main() {
	log.Println("🚀 Starting Auth Service v2...")

	// Подключение к БД
	if err := models.InitDB(); err != nil {
		log.Fatalf("❌ Database connection failed: %v", err)
	}
	defer models.DB.Close()

	// Запуск миграций
	if err := models.RunMigrations(); err != nil {
		log.Fatalf("❌ Migrations failed: %v", err)
	}

	// Создание обработчиков
	authHandler := handlers.NewAuthHandler()

	// Настройка маршрутов
	router := mux.NewRouter()

	// Публичные endpoints
	router.HandleFunc("/health", healthHandler).Methods("GET")
	router.HandleFunc("/auth/register", authHandler.Register).Methods("POST")
	router.HandleFunc("/auth/login", authHandler.Login).Methods("POST")

	// Сервисные endpoints (для других микросервисов)
	router.HandleFunc("/service/validate-token", authHandler.ValidateToken).Methods("POST")

	// CORS
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	port := getEnv("PORT", "8082")
	log.Printf("✅ Auth Service v2 starting on port %s", port)
	log.Printf("📋 Available endpoints:")
	log.Printf("  GET  /health")
	log.Printf("  POST /auth/register")
	log.Printf("  POST /auth/login")
	log.Printf("  POST /service/validate-token")

	log.Fatal(http.ListenAndServe(":"+port, c.Handler(router)))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":    "healthy",
		"service":   "auth-service-v2",
		"version":   "2.0.0",
		"timestamp": "2025-09-25T22:20:00Z",
	}

	// Проверка БД
	if err := models.DB.Ping(); err != nil {
		status["status"] = "unhealthy"
		status["database"] = "disconnected"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		status["database"] = "connected"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
