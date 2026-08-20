package user

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"backend-api/internal/database"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("email is already registered")
)

// ResourceDB handles database operations for the user resource via Supabase PostgREST
type ResourceDB struct {
	dbClient *database.Client
}

// NewResourceDB creates a ResourceDB instance connected to Supabase
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
	}
}

// FindByEmail retrieves a user by email directly from Supabase DB
func (r *ResourceDB) FindByEmail(ctx context.Context, email string) (*User, error) {
	if r.dbClient == nil {
		return nil, errors.New("database client is not initialized")
	}

	normalized := strings.ToLower(strings.TrimSpace(email))
	var results []User
	params := url.Values{}
	params.Set("email", "eq."+normalized)
	params.Set("limit", "1")

	err := r.dbClient.Query(ctx, "users", params, &results)
	if err != nil {
		return nil, fmt.Errorf("supabase user lookup error: %w", err)
	}
	if len(results) == 0 {
		return nil, ErrUserNotFound
	}
	return &results[0], nil
}

// FindByID retrieves a user by ID directly from Supabase DB
func (r *ResourceDB) FindByID(ctx context.Context, id string) (*User, error) {
	if r.dbClient == nil {
		return nil, errors.New("database client is not initialized")
	}

	var results []User
	params := url.Values{}
	params.Set("id", "eq."+id)
	params.Set("limit", "1")

	err := r.dbClient.Query(ctx, "users", params, &results)
	if err != nil {
		return nil, fmt.Errorf("supabase user lookup error: %w", err)
	}
	if len(results) == 0 {
		return nil, ErrUserNotFound
	}
	return &results[0], nil
}

// Create inserts a new user record directly into Supabase DB
func (r *ResourceDB) Create(ctx context.Context, user *User) error {
	if r.dbClient == nil {
		return errors.New("database client is not initialized")
	}

	var created []User
	err := r.dbClient.Insert(ctx, "users", user, &created)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "already registered") {
			return ErrUserAlreadyExists
		}
		return fmt.Errorf("supabase user creation error: %w", err)
	}
	if len(created) > 0 {
		*user = created[0]
	}
	return nil
}
