package user

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"backend-api/internal/model"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidEmail       = errors.New("invalid email address format")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters long")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountInactive    = errors.New("user account is inactive")
	ErrInvalidRole        = errors.New("invalid role specified")
	ErrInvalidToken       = errors.New("invalid or expired token")
)

var AllowedRoles = map[string]bool{
	"AP_MANAGER":         true,
	"FINANCE_CONTROLLER": true,
	"LOGISTICS_MANAGER":  true,
	"AUDITOR":            true,
	"ADMIN":              true,
}

// TokenClaims represents JWT token payload
type TokenClaims struct {
	UserID      string `json:"sub"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	CompanyName string `json:"company"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}

// Resource contains the business logic for the User domain
type Resource struct {
	db        *ResourceDB
	jwtSecret []byte
	tokenTTL  time.Duration
}

// NewResource creates a user Resource instance
func NewResource(db *ResourceDB, jwtSecret string) *Resource {
	return &Resource{
		db:        db,
		jwtSecret: []byte(jwtSecret),
		tokenTTL:  24 * time.Hour,
	}
}

// Register validates user input, hashes password, saves to DB, and returns auth response
func (r *Resource) Register(ctx context.Context, req RegisterRequest) (*AuthResponse, error) {
	normalizedEmail := strings.ToLower(strings.TrimSpace(req.Email))
	if _, err := mail.ParseAddress(normalizedEmail); err != nil {
		return nil, ErrInvalidEmail
	}

	if len(req.Password) < 8 {
		return nil, ErrPasswordTooShort
	}

	role := strings.ToUpper(strings.TrimSpace(req.Role))
	if role == "" {
		role = "AP_MANAGER"
	}
	if !AllowedRoles[role] {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRole, role)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "User"
	}

	company := strings.TrimSpace(req.CompanyName)
	if company == "" {
		company = "PT Enterprise Logistik"
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now().UTC()
	newUser := &User{
		ID:           model.NewUUID(),
		Email:        normalizedEmail,
		PasswordHash: string(hashedBytes),
		Name:         name,
		CompanyName:  company,
		Role:         role,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := r.db.Create(ctx, newUser); err != nil {
		return nil, err
	}

	token, err := r.generateToken(newUser)
	if err != nil {
		return nil, fmt.Errorf("token generation failed: %w", err)
	}

	return &AuthResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: int64(r.tokenTTL.Seconds()),
		User:      newUser.ToProfile(),
	}, nil
}

// Login authenticates a user and returns an auth response
func (r *Resource) Login(ctx context.Context, req LoginRequest) (*AuthResponse, error) {
	normalized := strings.ToLower(strings.TrimSpace(req.Email))
	u, err := r.db.FindByEmail(ctx, normalized)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if !u.IsActive {
		return nil, ErrAccountInactive
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	token, err := r.generateToken(u)
	if err != nil {
		return nil, fmt.Errorf("token generation failed: %w", err)
	}

	return &AuthResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: int64(r.tokenTTL.Seconds()),
		User:      u.ToProfile(),
	}, nil
}

// ValidateToken verifies JWT signature and expiry
func (r *Resource) ValidateToken(tokenString string) (*TokenClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	headerB64, payloadB64, signatureB64 := parts[0], parts[1], parts[2]
	unsignedToken := headerB64 + "." + payloadB64

	sig := r.computeHMAC(unsignedToken)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	if !hmac.Equal([]byte(signatureB64), []byte(sigB64)) {
		return nil, ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, ErrInvalidToken
	}

	var claims TokenClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, ErrInvalidToken
	}

	if time.Now().UTC().Unix() > claims.ExpiresAt {
		return nil, ErrInvalidToken
	}

	return &claims, nil
}

// GetProfile retrieves user profile by ID
func (r *Resource) GetProfile(ctx context.Context, id string) (*Profile, error) {
	u, err := r.db.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	p := u.ToProfile()
	return &p, nil
}

func (r *Resource) generateToken(u *User) (string, error) {
	now := time.Now().UTC()
	claims := TokenClaims{
		UserID:      u.ID,
		Email:       u.Email,
		Name:        u.Name,
		Role:        u.Role,
		CompanyName: u.CompanyName,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(r.tokenTTL).Unix(),
	}

	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}

	headerBytes, _ := json.Marshal(header)
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsignedToken := headerB64 + "." + payloadB64

	sig := r.computeHMAC(unsignedToken)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return unsignedToken + "." + sigB64, nil
}

func (r *Resource) computeHMAC(data string) []byte {
	h := hmac.New(sha256.New, r.jwtSecret)
	h.Write([]byte(data))
	return h.Sum(nil)
}
