package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AuthClient клиент для взаимодействия с Auth Service
type AuthClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// AuthClaims информация о пользователе из токена
type AuthClaims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

type ValidateTokenRequest struct {
	Token string `json:"token"`
}

type ValidateTokenResponse struct {
	Valid    bool   `json:"valid"`
	UserID   int    `json:"user_id,omitempty"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
	Role     string `json:"role,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func NewAuthClient() *AuthClient {
	return &AuthClient{
		baseURL: getEnv("AUTH_SERVICE_URL", "http://localhost:8082"),
		apiKey:  getEnv("SERVICE_API_KEY", "dev-service-api-key-for-local-testing"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// ValidateToken проверяет токен через Auth Service
func (ac *AuthClient) ValidateToken(ctx context.Context, token string) (*AuthClaims, error) {
	reqBody := ValidateTokenRequest{Token: token}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ac.baseURL+"/service/validate-token", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", ac.apiKey)

	resp, err := ac.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	var response ValidateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Valid {
		return nil, fmt.Errorf("invalid token: %s", response.Reason)
	}

	return &AuthClaims{
		UserID:   response.UserID,
		Username: response.Username,
		Email:    response.Email,
		Role:     response.Role,
	}, nil
}

// AuthMiddleware middleware для проверки авторизации
func (ac *AuthClient) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]
		claims, err := ac.ValidateToken(r.Context(), token)
		if err != nil {
			http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// Добавляем информацию о пользователе в контекст
		ctx := context.WithValue(r.Context(), "user", claims)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// RequireRole middleware для проверки конкретной роли
func (ac *AuthClient) RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value("user").(*AuthClaims)
			if !ok {
				http.Error(w, "User context not found", http.StatusInternalServerError)
				return
			}

			hasRole := false
			for _, role := range roles {
				if claims.Role == role || claims.Role == "admin" { // admin всегда имеет доступ
					hasRole = true
					break
				}
			}

			if !hasRole {
				http.Error(w, fmt.Sprintf("Insufficient permissions. Required: %v, Got: %s", roles, claims.Role), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
