package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Storage defines the interface for document file storage operations.
type Storage interface {
	Save(ctx context.Context, bucket string, filename string, r io.Reader) (string, error)
	Open(ctx context.Context, storagePath string) (io.ReadCloser, error)
	Delete(ctx context.Context, storagePath string) error
}

// LocalStorage implements Storage interface using local filesystem.
type LocalStorage struct {
	baseDir string
}

// NewLocalStorage creates a LocalStorage instance.
func NewLocalStorage(baseDir string) (*LocalStorage, error) {
	if baseDir == "" {
		baseDir = "storage"
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base storage directory: %w", err)
	}
	return &LocalStorage{baseDir: baseDir}, nil
}

func (s *LocalStorage) Save(ctx context.Context, bucket string, filename string, r io.Reader) (string, error) {
	bucketDir := filepath.Join(s.baseDir, bucket)
	if err := os.MkdirAll(bucketDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bucket directory: %w", err)
	}

	cleanFilename := filepath.Base(filename)
	targetPath := filepath.Join(bucketDir, cleanFilename)

	f, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to create target file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("failed to write file content: %w", err)
	}

	// Return relative storage path with forward slashes
	relPath := filepath.ToSlash(filepath.Join(bucket, cleanFilename))
	return relPath, nil
}

func (s *LocalStorage) Open(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	fullPath := filepath.Join(s.baseDir, filepath.FromSlash(storagePath))
	return os.Open(fullPath)
}

func (s *LocalStorage) Delete(ctx context.Context, storagePath string) error {
	fullPath := filepath.Join(s.baseDir, filepath.FromSlash(storagePath))
	return os.Remove(fullPath)
}

// SupabaseStorage implements Storage using Supabase Storage REST API.
type SupabaseStorage struct {
	supabaseURL    string
	serviceRoleKey string
	httpClient     *http.Client
	fallbackLocal  *LocalStorage
}

// NewSupabaseStorage creates a SupabaseStorage instance.
func NewSupabaseStorage(supabaseURL, serviceRoleKey string, fallback *LocalStorage) *SupabaseStorage {
	return &SupabaseStorage{
		supabaseURL:    strings.TrimRight(supabaseURL, "/"),
		serviceRoleKey: serviceRoleKey,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		fallbackLocal:  fallback,
	}
}

func (s *SupabaseStorage) Save(ctx context.Context, bucket string, filename string, r io.Reader) (string, error) {
	if s.supabaseURL == "" || s.serviceRoleKey == "" {
		if s.fallbackLocal != nil {
			return s.fallbackLocal.Save(ctx, bucket, filename, r)
		}
		return "", fmt.Errorf("supabase storage not configured and no local fallback")
	}

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, r); err != nil {
		return "", fmt.Errorf("failed to read buffer: %w", err)
	}

	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.supabaseURL, bucket, filename)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return "", fmt.Errorf("failed to create supabase storage request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.serviceRoleKey)
	req.Header.Set("apikey", s.serviceRoleKey)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("x-upsert", "true")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		// Fallback to local storage on network/connection issue
		if s.fallbackLocal != nil {
			return s.fallbackLocal.Save(ctx, bucket, filename, bytes.NewReader(buf.Bytes()))
		}
		return "", fmt.Errorf("failed to execute supabase storage upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusConflict {
		// If cloud bucket is not initialized yet, fallback to local
		if s.fallbackLocal != nil {
			return s.fallbackLocal.Save(ctx, bucket, filename, bytes.NewReader(buf.Bytes()))
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("supabase storage error (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return fmt.Sprintf("%s/%s", bucket, filename), nil
}

func (s *SupabaseStorage) Open(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	if s.fallbackLocal != nil {
		if rc, err := s.fallbackLocal.Open(ctx, storagePath); err == nil {
			return rc, nil
		}
	}

	if s.supabaseURL == "" || s.serviceRoleKey == "" {
		return nil, fmt.Errorf("storage not configured")
	}

	url := fmt.Sprintf("%s/storage/v1/object/public/%s", s.supabaseURL, storagePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceRoleKey)
	req.Header.Set("apikey", s.serviceRoleKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to fetch from supabase storage: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (s *SupabaseStorage) Delete(ctx context.Context, storagePath string) error {
	if s.fallbackLocal != nil {
		_ = s.fallbackLocal.Delete(ctx, storagePath)
	}
	return nil
}
