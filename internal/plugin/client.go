package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// APIError an API error.
type APIError struct {
	StatusCode int
	Message    string
}

func (a *APIError) Error() string {
	return fmt.Sprintf("%d: %s", a.StatusCode, a.Message)
}

// Client for the plugin service.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a plugin service client.
func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)},
	}
}

// Create creates a plugin.
func (c *Client) Create(ctx context.Context, p Plugin) error {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("failed to parse base URL: %w", err)
	}

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshall: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL.String(), bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call API: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)

		return &APIError{
			Message:    string(body),
			StatusCode: resp.StatusCode,
		}
	}

	return nil
}

// Update updates a plugin.
func (c *Client) Update(ctx context.Context, p Plugin) error {
	if p.ID == "" {
		return errors.New("missing plugin ID")
	}

	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("failed to parse base URL: %w", err)
	}

	endpoint, err := baseURL.Parse(path.Join(baseURL.Path, p.ID))
	if err != nil {
		return fmt.Errorf("failed to parse endpoint URL: %w", err)
	}

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshall: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call API: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			Message:    string(body),
			StatusCode: resp.StatusCode,
		}
	}

	return nil
}

// GetByName gets a plugin by name.
func (c *Client) GetByName(ctx context.Context, name string) (*Plugin, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	query := baseURL.Query()
	query.Set("name", name)
	query.Set("filterHidden", "false")
	baseURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call API: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return nil, &APIError{
			Message:    string(body),
			StatusCode: resp.StatusCode,
		}
	}

	var plgs []Plugin
	err = json.Unmarshal(body, &plgs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarchall data: %w", err)
	}

	if len(plgs) != 1 {
		return nil, fmt.Errorf("failed to get plugin: %s", name)
	}

	return &plgs[0], nil
}

// ListStates gets the state of all plugins, hidden and disabled plugins included.
func (c *Client) ListStates(ctx context.Context) ([]State, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	endpoint, err := baseURL.Parse(path.Join(baseURL.Path, "states"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse endpoint URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// The response contains all the plugins: the default timeout can be too short.
	client := &http.Client{Timeout: time.Minute, Transport: c.httpClient.Transport}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call API: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return nil, &APIError{
			Message:    string(body),
			StatusCode: resp.StatusCode,
		}
	}

	var states []State
	if err = json.Unmarshal(body, &states); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	return states, nil
}

// Patch updates only the fields of the patch for the plugin id.
func (c *Client) Patch(ctx context.Context, id string, patch Patch) error {
	if id == "" {
		return errors.New("missing plugin ID")
	}

	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("failed to parse base URL: %w", err)
	}

	endpoint, err := baseURL.Parse(path.Join(baseURL.Path, id))
	if err != nil {
		return fmt.Errorf("failed to parse endpoint URL: %w", err)
	}

	data, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint.String(), bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call API: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)

		return &APIError{
			Message:    string(body),
			StatusCode: resp.StatusCode,
		}
	}

	return nil
}

// ListBlacklist gets the blacklisted repositories.
func (c *Client) ListBlacklist(ctx context.Context) ([]BlacklistEntry, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	endpoint, err := baseURL.Parse(path.Join(baseURL.Path, "blacklist"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse endpoint URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call API: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return nil, &APIError{
			Message:    string(body),
			StatusCode: resp.StatusCode,
		}
	}

	var entries []BlacklistEntry
	if err = json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	return entries, nil
}
