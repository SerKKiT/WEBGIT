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
	log.Println("Starting VOD Service...")

	// Database connection
	log.Println("Connecting to database...")
	db, err := ConnectDB()
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Initialize storage
	log.Println("Initializing MinIO storage...")
	storage, err := NewStorage()
	if err != nil {
		log.Fatal("Failed to initialize storage:", err)
	}

	log.Println("Testing MinIO connection...")
	if err := storage.TestConnection(); err != nil {
		log.Fatal("MinIO connection test failed:", err)
	}

	// Create handlers
	h := &Handlers{
		db:      db,
		storage: storage,
	}

	// Setup router
	r := mux.NewRouter()

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	// Публичные маршруты (без авторизации)
	public := r.PathPrefix("/api/v1").Subrouter()
	public.HandleFunc("/recordings", h.ListRecordings).Methods("GET")
	public.HandleFunc("/recordings/{streamId}", h.GetRecording).Methods("GET")
	public.HandleFunc("/recordings/{streamId}/stream", h.GetStreamURL).Methods("GET")
	public.HandleFunc("/recordings/{streamId}/thumbnail", h.GetThumbnailURL).Methods("GET")

	// Защищенные маршруты (требуют авторизации)
	protected := r.PathPrefix("/api/v1").Subrouter()
	protected.HandleFunc("/recordings", h.CreateRecording).Methods("POST")
	protected.HandleFunc("/recordings/{streamId}", h.UpdateRecording).Methods("PUT")
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
		log.Printf("✅ VOD Service starting on port %s", port)
		log.Printf("📋 Available endpoints:")
		log.Printf("  GET  /api/v1/recordings")
		log.Printf("  POST /api/v1/recordings (protected)")
		log.Printf("  GET  /api/v1/recordings/{id}")
		log.Printf("  PUT  /api/v1/recordings/{id} (protected)")
		log.Printf("  DEL  /api/v1/recordings/{id} (protected)")
		log.Printf("  GET  /health")

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
