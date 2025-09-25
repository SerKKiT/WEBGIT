package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"web/auth-service/models"
	"web/auth-service/utils"
)

type AuthHandler struct {
	serviceAPIKey string
}

func NewAuthHandler() *AuthHandler {
	return &AuthHandler{
		serviceAPIKey: getEnv("SERVICE_API_KEY", "dev-service-api-key-for-local-testing"),
	}
}

// Register регистрирует нового пользователя
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Валидация входных данных
	if req.Username == "" || req.Email == "" || req.Password == "" {
		http.Error(w, "Username, email, and password are required", http.StatusBadRequest)
		return
	}

	if len(req.Password) < 6 {
		http.Error(w, "Password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	// Создание пользователя
	user, err := models.CreateUser(req)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			http.Error(w, "User with this email or username already exists", http.StatusConflict)
			return
		}
		log.Printf("❌ Failed to create user: %v", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Генерация токена
	token, err := utils.GenerateToken(user.ID, user.Username, user.Email, user.Role)
	if err != nil {
		log.Printf("❌ Failed to generate token: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	response := models.LoginResponse{
		AccessToken: token,
		User:        *user,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)

	log.Printf("✅ User registered: %s (%s) - role: %s", user.Username, user.Email, user.Role)
}

// Login авторизует пользователя
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Валидация входных данных
	if req.Email == "" || req.Password == "" {
		http.Error(w, "Email and password are required", http.StatusBadRequest)
		return
	}

	// Поиск пользователя
	user, passwordHash, err := models.GetUserByEmail(req.Email)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Проверка пароля
	if !models.ValidatePassword(passwordHash, req.Password) {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Генерация токена
	token, err := utils.GenerateToken(user.ID, user.Username, user.Email, user.Role)
	if err != nil {
		log.Printf("❌ Failed to generate token: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	response := models.LoginResponse{
		AccessToken: token,
		User:        *user,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("✅ User logged in: %s (%s) - role: %s", user.Username, user.Email, user.Role)
}

// ValidateToken проверяет токен (для межсервисного взаимодействия)
func (h *AuthHandler) ValidateToken(w http.ResponseWriter, r *http.Request) {
	// Проверка API ключа
	apiKey := r.Header.Get("X-API-Key")
	if apiKey != h.serviceAPIKey {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // 200 OK для корректной обработки
		json.NewEncoder(w).Encode(models.ValidateTokenResponse{
			Valid:  false,
			Reason: "Invalid API key",
		})
		return
	}

	var req models.ValidateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(models.ValidateTokenResponse{
			Valid:  false,
			Reason: "Invalid request body",
		})
		return
	}

	// Валидация JWT токена
	claims, err := utils.ValidateToken(req.Token)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(models.ValidateTokenResponse{
			Valid:  false,
			Reason: "Invalid or expired token",
		})
		return
	}

	// Проверка что пользователь всё ещё активен
	user, err := models.GetUserByID(claims.UserID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(models.ValidateTokenResponse{
			Valid:  false,
			Reason: "User not found or inactive",
		})
		return
	}

	// Успешная валидация
	response := models.ValidateTokenResponse{
		Valid:    true,
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
		Role:     user.Role,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
