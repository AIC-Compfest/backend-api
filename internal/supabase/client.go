package supabase

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type Client struct { baseURL, publishableKey string; httpClient *http.Client }

func NewClient(baseURL, publishableKey string) *Client { return &Client{baseURL: strings.TrimRight(baseURL, "/"), publishableKey: publishableKey, httpClient: http.DefaultClient} }
func NewClientWithHTTP(baseURL, publishableKey string, httpClient *http.Client) *Client { c := NewClient(baseURL, publishableKey); c.httpClient = httpClient; return c }

func (c *Client) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/rest/v1/", nil)
	if err != nil { return err }
	req.Header.Set("apikey", c.publishableKey)
	resp, err := c.httpClient.Do(req)
	if err != nil { return fmt.Errorf("supabase REST request: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return fmt.Errorf("supabase REST returned %s", resp.Status) }
	return nil
}

func (c *Client) Request(ctx context.Context, method, path string, body string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, strings.NewReader(body))
	if err != nil { return nil, err }
	req.Header.Set("apikey", c.publishableKey); req.Header.Set("Authorization", "Bearer "+c.publishableKey); req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}
