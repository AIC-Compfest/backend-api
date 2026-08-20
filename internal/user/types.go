package user

import "time"

// User represents a user account record
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"password_hash,omitempty"`
	Name         string    `json:"name"`
	CompanyName  string    `json:"company_name"`
	Role         string    `json:"role"` // AP_MANAGER, FINANCE_CONTROLLER, LOGISTICS_MANAGER, AUDITOR, ADMIN
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Profile is the public safe representation of User
type Profile struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	CompanyName string    `json:"company_name"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

// ToProfile maps User to Profile
func (u *User) ToProfile() Profile {
	return Profile{
		ID:          u.ID,
		Email:       u.Email,
		Name:        u.Name,
		CompanyName: u.CompanyName,
		Role:        u.Role,
		CreatedAt:   u.CreatedAt,
	}
}

// RegisterRequest payload
type RegisterRequest struct {
	Email       string `json:"email" binding:"required"`
	Password    string `json:"password" binding:"required"`
	Name        string `json:"name" binding:"required"`
	CompanyName string `json:"company_name"`
	Role        string `json:"role"`
}

// LoginRequest payload
type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// AuthResponse payload
type AuthResponse struct {
	Token     string  `json:"token"`
	TokenType string  `json:"token_type"`
	ExpiresIn int64   `json:"expires_in"`
	User      Profile `json:"user"`
}
