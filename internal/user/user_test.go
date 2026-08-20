package user

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"backend-api/internal/config"
	"backend-api/internal/database"
)

func TestUserRegistrationAndLogin(t *testing.T) {
	cfg, _ := config.Load()
	dbClient := database.NewClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	db := NewResourceDB(dbClient)
	res := NewResource(db, "test-secret-key-12345")
	ctx := context.Background()

	testEmail := fmt.Sprintf("test.user.%d_%d@enterprise.com", os.Getpid(), time.Now().UnixNano())

	// 1. Register new user directly into Supabase
	regReq := RegisterRequest{
		Email:       testEmail,
		Password:    "SecurePass2026!",
		Name:        "Test Auditor",
		CompanyName: "PT Logistik Jaya",
		Role:        "AUDITOR",
	}

	authResp, err := res.Register(ctx, regReq)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if authResp.Token == "" {
		t.Errorf("Expected non-empty JWT token")
	}
	if authResp.User.Email != testEmail {
		t.Errorf("Expected email '%s', got '%s'", testEmail, authResp.User.Email)
	}

	// 2. Validate token
	claims, err := res.ValidateToken(authResp.Token)
	if err != nil {
		t.Fatalf("Token validation failed: %v", err)
	}
	if claims.Role != "AUDITOR" {
		t.Errorf("Expected role 'AUDITOR', got '%s'", claims.Role)
	}

	// 3. Login with correct credentials
	loginReq := LoginRequest{
		Email:    testEmail,
		Password: "SecurePass2026!",
	}
	loginResp, err := res.Login(ctx, loginReq)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if loginResp.User.ID != authResp.User.ID {
		t.Errorf("Expected user ID '%s', got '%s'", authResp.User.ID, loginResp.User.ID)
	}

	// 4. Adversarial: Wrong password
	badLogin := LoginRequest{
		Email:    testEmail,
		Password: "WrongPassword!",
	}
	_, err = res.Login(ctx, badLogin)
	if err == nil {
		t.Errorf("Expected error on invalid password, got nil")
	}

	// 5. Adversarial: Duplicate registration
	_, err = res.Register(ctx, regReq)
	if err == nil {
		t.Errorf("Expected error on duplicate email registration, got nil")
	}
}
