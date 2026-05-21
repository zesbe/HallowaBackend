package supabase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a thin REST wrapper for PostgREST + helpers for our schema.
type Client struct {
	baseURL string
	apiKey  string
	hc      *http.Client
}

func New(baseURL, serviceRoleKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  serviceRoleKey,
		hc:      &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any, prefer string) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if prefer != "" {
		req.Header.Set("Prefer", prefer)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("supabase %s %s: %d %s", method, path, resp.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// ---------------- backend_servers ----------------

type BackendServer struct {
	ID             string         `json:"id,omitempty"`
	ServerName     string         `json:"server_name"`
	ServerURL      string         `json:"server_url"`
	ServerType     string         `json:"server_type"`
	Region         string         `json:"region"`
	MaxCapacity    int            `json:"max_capacity"`
	CurrentLoad    int            `json:"current_load"`
	IsActive       bool           `json:"is_active"`
	IsHealthy      bool           `json:"is_healthy"`
	Priority       int            `json:"priority"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	LastHealthAt   *string        `json:"last_health_check,omitempty"`
	HealthFailures int            `json:"health_check_failures"`
}

func (c *Client) FindServerByURL(ctx context.Context, serverURL string) (*BackendServer, error) {
	q := url.Values{}
	q.Set("server_url", "eq."+serverURL)
	q.Set("select", "*")
	q.Set("limit", "1")
	var rows []BackendServer
	if err := c.do(ctx, "GET", "/rest/v1/backend_servers?"+q.Encode(), nil, &rows, ""); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (c *Client) UpsertServer(ctx context.Context, s BackendServer) (*BackendServer, error) {
	var rows []BackendServer
	if s.ID != "" {
		path := "/rest/v1/backend_servers?id=eq." + url.QueryEscape(s.ID)
		if err := c.do(ctx, "PATCH", path, s, &rows, "return=representation"); err != nil {
			return nil, err
		}
	} else {
		if err := c.do(ctx, "POST", "/rest/v1/backend_servers", s, &rows, "return=representation"); err != nil {
			return nil, err
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("upsert returned no rows")
	}
	return &rows[0], nil
}

func (c *Client) UpdateServerHealth(ctx context.Context, id string, healthy bool, load int) error {
	path := "/rest/v1/backend_servers?id=eq." + url.QueryEscape(id)
	body := map[string]any{
		"is_healthy":         healthy,
		"current_load":       load,
		"last_health_check":  time.Now().UTC().Format(time.RFC3339Nano),
		"updated_at":         time.Now().UTC().Format(time.RFC3339Nano),
	}
	return c.do(ctx, "PATCH", path, body, nil, "")
}
