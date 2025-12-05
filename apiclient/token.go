package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// KeycloakTokenFetcher fetches a bearer token via client_credentials.
type KeycloakTokenFetcher struct {
	httpClient   *http.Client
	tokenURL     string
	clientID     string
	clientSecret string
}

// NewKeycloakTokenFetcher creates a fetcher with explicit config.
func NewKeycloakTokenFetcher(httpClient *http.Client, tokenURL, clientID, clientSecret string) *KeycloakTokenFetcher {
	if tokenURL == "" || clientID == "" || clientSecret == "" {
		return nil
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &KeycloakTokenFetcher{
		httpClient:   httpClient,
		tokenURL:     strings.TrimSpace(tokenURL),
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
	}
}

// NewKeycloakTokenFetcherFromEnv builds a fetcher from env:
// KEYCLOAK_BASE_URL + KEYCLOAK_REALM, and AUTH_CLIENT_ID / AUTH_CLIENT_SECRET.
func NewKeycloakTokenFetcherFromEnv() *KeycloakTokenFetcher {
	base := strings.TrimSpace(os.Getenv("KEYCLOAK_BASE_URL"))
	realm := strings.TrimSpace(os.Getenv("KEYCLOAK_REALM"))

	if base == "" || realm == "" {
		return nil
	}

	b := strings.TrimSuffix(base, "/")
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", b, url.PathEscape(realm))

	return NewKeycloakTokenFetcher(
		nil,
		tokenURL,
		os.Getenv("AUTH_CLIENT_ID"),
		os.Getenv("AUTH_CLIENT_SECRET"),
	)
}

// Fetch retrieves an access token string (without "Bearer " prefix).
func (f *KeycloakTokenFetcher) Fetch(ctx context.Context) (string, error) {
	if f == nil {
		return "", errors.New("fetcher is nil")
	}

	if f.tokenURL == "" || f.clientID == "" || f.clientSecret == "" {
		return "", errors.New("keycloak config missing")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", f.clientID)
	form.Set("client_secret", f.clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token request %s -> %d: %s", f.tokenURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	//nolint:tagliatelle // OAuth response uses snake_case keys
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("token parse error: %w; body=%s", err, string(body))
	}

	if tok.AccessToken == "" {
		return "", errors.New("empty access_token in response")
	}

	return tok.AccessToken, nil
}
