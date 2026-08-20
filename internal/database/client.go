package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client handles communication with the Supabase PostgREST database layer.
type Client struct {
	baseURL        string
	serviceRoleKey string
	httpClient     *http.Client
}

// NewClient creates a new Supabase database client.
func NewClient(baseURL, serviceRoleKey string) *Client {
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		serviceRoleKey: serviceRoleKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Query performs a GET request on a Supabase table with query parameters and decodes into target.
func (c *Client) Query(ctx context.Context, table string, params url.Values, target interface{}) error {
	endpoint := fmt.Sprintf("%s/rest/v1/%s", c.baseURL, table)
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("query to %s failed: %w", table, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase query error (%d) on table '%s': %s", resp.StatusCode, table, string(bodyBytes))
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// Count returns the total number of matching rows in a table.
func (c *Client) Count(ctx context.Context, table string, params url.Values) (int64, error) {
	endpoint := fmt.Sprintf("%s/rest/v1/%s", c.baseURL, table)
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create count request: %w", err)
	}

	c.setHeaders(req)
	req.Header.Set("Prefer", "count=exact")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("count request to %s failed: %w", table, err)
	}
	defer resp.Body.Close()

	contentRange := resp.Header.Get("Content-Range")
	if contentRange == "" {
		return 0, nil
	}

	// Format: "0-19/20" or "*/20"
	parts := strings.Split(contentRange, "/")
	if len(parts) == 2 {
		total, err := strconv.ParseInt(parts[1], 10, 64)
		if err == nil {
			return total, nil
		}
	}

	return 0, nil
}

// Insert inserts a new record or records into a table and optionally returns representation.
func (c *Client) Insert(ctx context.Context, table string, payload interface{}, target interface{}) error {
	endpoint := fmt.Sprintf("%s/rest/v1/%s", c.baseURL, table)

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create insert request: %w", err)
	}

	c.setHeaders(req)
	req.Header.Set("Prefer", "return=representation")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("insert to %s failed: %w", table, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase insert error (%d) on table '%s': %s", resp.StatusCode, table, string(bodyBytes))
	}

	if target != nil {
		return json.NewDecoder(resp.Body).Decode(target)
	}
	return nil
}

// Update updates records matching query params in a table.
func (c *Client) Update(ctx context.Context, table string, params url.Values, payload interface{}, target interface{}) error {
	endpoint := fmt.Sprintf("%s/rest/v1/%s", c.baseURL, table)
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal update payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create update request: %w", err)
	}

	c.setHeaders(req)
	req.Header.Set("Prefer", "return=representation")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update to %s failed: %w", table, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase update error (%d) on table '%s': %s", resp.StatusCode, table, string(bodyBytes))
	}

	if target != nil {
		return json.NewDecoder(resp.Body).Decode(target)
	}
	return nil
}

// Delete removes records matching query params from a table.
func (c *Client) Delete(ctx context.Context, table string, params url.Values) error {
	endpoint := fmt.Sprintf("%s/rest/v1/%s", c.baseURL, table)
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete on %s failed: %w", table, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase delete error (%d) on table '%s': %s", resp.StatusCode, table, string(bodyBytes))
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("apikey", c.serviceRoleKey)
	req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
	req.Header.Set("Content-Type", "application/json")
}
