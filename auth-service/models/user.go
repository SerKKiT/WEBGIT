package models

import (
	"database/sql"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Не включаем password_hash в JSON
}

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"` // Опционально, по умолчанию viewer
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken string `json:"access_token"`
	User        User   `json:"user"`
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

// CreateUser создает нового пользователя
func CreateUser(req RegisterRequest) (*User, error) {
	// Хеширование пароля
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Роль по умолчанию
	if req.Role == "" {
		req.Role = "viewer"
	}

	// Валидация роли
	if req.Role != "admin" && req.Role != "streamer" && req.Role != "viewer" {
		return nil, fmt.Errorf("invalid role: %s", req.Role)
	}

	// Создание пользователя
	query := `
        INSERT INTO users (username, email, password_hash, role) 
        VALUES ($1, $2, $3, $4)
        RETURNING id, username, email, role, is_active, created_at, updated_at
    `

	user := &User{}
	err = DB.QueryRow(query, req.Username, req.Email, hashedPassword, req.Role).Scan(
		&user.ID, &user.Username, &user.Email, &user.Role,
		&user.IsActive, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// GetUserByEmail получает пользователя по email
func GetUserByEmail(email string) (*User, string, error) {
	user := &User{}
	var passwordHash string

	query := `
        SELECT id, username, email, password_hash, role, is_active, created_at, updated_at
        FROM users 
        WHERE email = $1 AND is_active = true
    `

	err := DB.QueryRow(query, email).Scan(
		&user.ID, &user.Username, &user.Email, &passwordHash, &user.Role,
		&user.IsActive, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("user not found")
		}
		return nil, "", fmt.Errorf("database error: %w", err)
	}

	return user, passwordHash, nil
}

// GetUserByID получает пользователя по ID
func GetUserByID(id int) (*User, error) {
	user := &User{}

	query := `
        SELECT id, username, email, role, is_active, created_at, updated_at
        FROM users 
        WHERE id = $1 AND is_active = true
    `

	err := DB.QueryRow(query, id).Scan(
		&user.ID, &user.Username, &user.Email, &user.Role,
		&user.IsActive, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	return user, nil
}

// ValidatePassword проверяет пароль пользователя
func ValidatePassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}
