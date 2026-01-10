// Package client provides a Go client for the Pastey paste service.
//
// Basic usage:
//
//	c := client.New() // uses default https://ig.lc
//	url, err := c.Create(ctx, []byte("hello world"))
//	content, err := c.Get(ctx, "abc1234")
package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the default Pastey service URL.
	DefaultBaseURL = "https://ig.lc"

	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 30 * time.Second

	// MaxPayloadSize is the maximum paste size (5MB).
	MaxPayloadSize = 5_000_000
)

// Client is a Pastey API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL sets a custom base URL for the Pastey service.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimSuffix(baseURL, "/")
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// New creates a new Pastey client with the given options.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// CreateOptions configures paste creation.
type CreateOptions struct {
	// Secure generates a longer (32 char) ID instead of the default 7 char ID.
	Secure bool
}

// Create uploads content and returns the paste URL.
func (c *Client) Create(ctx context.Context, content []byte) (string, error) {
	return c.CreateWithOptions(ctx, content, CreateOptions{})
}

// CreateWithOptions uploads content with custom options and returns the paste URL.
func (c *Client) CreateWithOptions(ctx context.Context, content []byte, opts CreateOptions) (string, error) {
	if len(content) == 0 {
		return "", &Error{Code: ErrEmptyContent, Message: "content cannot be empty"}
	}
	if len(content) > MaxPayloadSize {
		return "", &Error{Code: ErrPayloadTooLarge, Message: fmt.Sprintf("content exceeds maximum size of %d bytes", MaxPayloadSize)}
	}

	endpoint := c.baseURL + "/create"
	if opts.Secure {
		endpoint += "?secure=true"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(content))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusCreated:
		return strings.TrimSpace(string(body)), nil
	case http.StatusTooManyRequests:
		return "", &Error{Code: ErrRateLimited, Message: strings.TrimSpace(string(body))}
	case http.StatusRequestEntityTooLarge:
		return "", &Error{Code: ErrPayloadTooLarge, Message: strings.TrimSpace(string(body))}
	case http.StatusForbidden:
		return "", &Error{Code: ErrBlacklisted, Message: strings.TrimSpace(string(body))}
	case http.StatusBadRequest:
		return "", &Error{Code: ErrBadRequest, Message: strings.TrimSpace(string(body))}
	default:
		return "", &Error{Code: ErrServer, Message: fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
}

// Get retrieves a paste by its identifier.
// The identifier can be either a full URL (https://ig.lc/abc123) or just the ID (abc123).
func (c *Client) Get(ctx context.Context, identifier string) ([]byte, error) {
	// Handle full URLs by extracting the identifier
	id := identifier
	if strings.HasPrefix(identifier, "http://") || strings.HasPrefix(identifier, "https://") {
		parsed, err := url.Parse(identifier)
		if err != nil {
			return nil, fmt.Errorf("parsing URL: %w", err)
		}
		id = strings.TrimPrefix(parsed.Path, "/")
	}

	if id == "" {
		return nil, &Error{Code: ErrBadRequest, Message: "identifier cannot be empty"}
	}

	endpoint := c.baseURL + "/" + id

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, &Error{Code: ErrNotFound, Message: "paste not found or expired"}
	case http.StatusTooManyRequests:
		return nil, &Error{Code: ErrRateLimited, Message: strings.TrimSpace(string(body))}
	default:
		return nil, &Error{Code: ErrServer, Message: fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
}
