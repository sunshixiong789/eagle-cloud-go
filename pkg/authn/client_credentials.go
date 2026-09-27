package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

type ClientCredentialsConfig struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

// ClientCredentials caches short-lived Keycloak client_credentials tokens.
type ClientCredentials struct {
	config ClientCredentialsConfig
	mu     sync.Mutex
	token  string
	expiry time.Time
}

func NewClientCredentials(config ClientCredentialsConfig) (*ClientCredentials, error) {
	if config.TokenURL == "" || config.ClientID == "" || config.ClientSecret == "" {
		return nil, errors.New("authn: client credentials token URL, client ID and secret are required")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &ClientCredentials{config: config}, nil
}

func (c *ClientCredentials) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expiry) > 30*time.Second {
		return c.token, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.config.ClientID},
		"client_secret": {c.config.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("authn: create service token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("authn: request service token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("authn: token endpoint returned %s", resp.Status)
	}
	var payload struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("authn: decode service token: %w", err)
	}
	if payload.AccessToken == "" {
		return "", errors.New("authn: token endpoint returned an empty access token")
	}
	expiresIn := int64(60)
	if len(payload.ExpiresIn) > 0 {
		var number json.Number
		if err := json.Unmarshal(payload.ExpiresIn, &number); err == nil {
			if parsed, err := strconv.ParseInt(number.String(), 10, 64); err == nil && parsed > 0 {
				expiresIn = parsed
			}
		}
	}
	c.token = payload.AccessToken
	c.expiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return c.token, nil
}

// Client authenticates an outgoing Kratos request with a cached service token.
func (c *ClientCredentials) Client() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			token, err := c.Token(ctx)
			if err != nil {
				return nil, err
			}
			tr, ok := transport.FromClientContext(ctx)
			if !ok {
				return nil, errors.New("authn: missing client transport context")
			}
			tr.RequestHeader().Set("Authorization", "Bearer "+token)
			return handler(ctx, req)
		}
	}
}
