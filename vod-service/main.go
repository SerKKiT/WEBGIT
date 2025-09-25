package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

func main() {
	log.Println("Starting VOD Service with Auth integration...")

	// Database connection
	db, err := ConnectDB()
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Initialize storage
	storage, err := NewStorage()
	if err != nil {
		log.Fatal("Failed to initialize storage:", err)
	}

	if err := storage.TestConnection(); err != nil {
		log.Fatal("MinIO connection test failed:", err)
	}

	// Initialize auth client
	authClient := NewAuthClient()

	// Create handlers
	h := &Handlers{
		db:         db,
		storage:    storage,
		authClient: authClient, // ✅ ДОБАВЛЕНО
	}

	// Setup router
	r := mux.NewRouter()

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	// ===================================
	// ПУБЛИЧНЫЕ ENDPOINTS (БЕЗ АВТОРИЗАЦИИ)
	// ===================================
	public := r.PathPrefix("/api/v1").Subrouter()
	public.HandleFunc("/recordings", h.ListRecordings).Methods("GET")
	public.HandleFunc("/recordings/{streamId}", h.GetRecording).Methods("GET")
	public.HandleFunc("/recordings/{streamId}/stream", h.GetStreamURL).Methods("GET")
	public.HandleFunc("/recordings/{streamId}/thumbnail", h.GetThumbnailURL).Methods("GET")

	// ===================================
	// ЗАЩИЩЕННЫЕ ENDPOINTS (ТРЕБУЮТ АВТОРИЗАЦИИ)
	// ===================================
	protected := r.PathPrefix("/api/v1").Subrouter()
	protected.Use(authClient.AuthMiddleware) // ✅ Все protected endpoints требуют токен

	// Обновление записей - только admin и streamer (только свои записи)
	protected.HandleFunc("/recordings/{streamId}", h.UpdateRecording).Methods("PUT")

	// Удаление записей - только admin и streamer (только свои записи)
	protected.HandleFunc("/recordings/{streamId}", h.DeleteRecording).Methods("DELETE")

	// Health check
	r.HandleFunc("/health", h.HealthCheck).Methods("GET")

	port := getEnv("PORT", "8081")
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      corsHandler.Handler(r),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("✅ VOD Service with Auth starting on port %s", port)
		log.Printf("📋 Endpoints:")
		log.Printf("  PUBLIC:")
		log.Printf("    GET  /api/v1/recordings")
		log.Printf("    GET  /api/v1/recordings/{id}")
		log.Printf("  PROTECTED (require Bearer token):")
		log.Printf("    POST /api/v1/recordings (admin, streamer only)")
		log.Printf("    PUT  /api/v1/recordings/{id}")
		log.Printf("    DEL  /api/v1/recordings/{id}")
		log.Printf("  AUTH SERVICE: %s", getEnv("AUTH_SERVICE_URL", "http://localhost:8082"))

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed to start:", err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down VOD Service...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("VOD Service stopped")
}
