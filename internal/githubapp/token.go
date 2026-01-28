package githubapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const (
	githubAPIBaseURL        = "https://api.github.com"
	githubAPIVersionDefault = "2022-11-28"
	jwtExpiry               = 9 * time.Minute
	jwtIssuedAtSkew         = 30 * time.Second
	tokenRefreshThreshold   = 2 * time.Minute
)

type TokenProvider struct {
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	client         *http.Client

	mu        sync.Mutex
	accessTok string
	expiresAt time.Time
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type tokenSource struct {
	ctx      context.Context
	provider *TokenProvider
}

var (
	defaultProviderOnce sync.Once
	defaultProvider     *TokenProvider
	errDefaultProvider  error
)

// DefaultProvider returns a cached provider built from env.
func DefaultProvider() (*TokenProvider, error) {
	defaultProviderOnce.Do(func() {
		defaultProvider, errDefaultProvider = NewTokenProviderFromEnv()
	})

	return defaultProvider, errDefaultProvider
}

// HasEnv reports whether all GitHub App env vars are set.
func HasEnv() bool {
	return strings.TrimSpace(os.Getenv("GIT_OAUTH_CLIENTID")) != "" &&
		strings.TrimSpace(os.Getenv("GIT_OAUTH_INSTALLATION_ID")) != "" &&
		strings.TrimSpace(os.Getenv("GIT_OAUTH_SECRET")) != ""
}

// NewTokenProviderFromEnv builds a provider from env.
func NewTokenProviderFromEnv() (*TokenProvider, error) {
	appIDRaw := strings.TrimSpace(os.Getenv("GIT_OAUTH_CLIENTID"))
	installIDRaw := strings.TrimSpace(os.Getenv("GIT_OAUTH_INSTALLATION_ID"))
	secretRaw := strings.TrimSpace(os.Getenv("GIT_OAUTH_SECRET"))

	if appIDRaw == "" || installIDRaw == "" || secretRaw == "" {
		return nil, errors.New("GIT_OAUTH_CLIENTID, GIT_OAUTH_INSTALLATION_ID, and GIT_OAUTH_SECRET must all be set")
	}

	appID, err := strconv.ParseInt(appIDRaw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid GIT_OAUTH_CLIENTID: %w", err)
	}

	installationID, err := strconv.ParseInt(installIDRaw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid GIT_OAUTH_INSTALLATION_ID: %w", err)
	}

	privateKey, err := parsePrivateKey(secretRaw)
	if err != nil {
		return nil, err
	}

	return &TokenProvider{
		appID:          appID,
		installationID: installationID,
		privateKey:     privateKey,
		client:         &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// TokenSource exposes the provider as oauth2.TokenSource.
func (p *TokenProvider) TokenSource(ctx context.Context) oauth2.TokenSource {
	return &tokenSource{ctx: ctx, provider: p}
}

// Token returns a cached installation token, refreshes near expiry.
func (p *TokenProvider) Token(ctx context.Context) (string, time.Time, error) {
	p.mu.Lock()

	if p.accessTok != "" && time.Until(p.expiresAt) > tokenRefreshThreshold {
		token := p.accessTok
		exp := p.expiresAt
		p.mu.Unlock()

		return token, exp, nil
	}

	p.mu.Unlock()

	return p.refreshToken(ctx)
}

// refreshToken fetches a new installation token from GitHub.
func (p *TokenProvider) refreshToken(ctx context.Context) (string, time.Time, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.accessTok != "" && time.Until(p.expiresAt) > tokenRefreshThreshold {
		return p.accessTok, p.expiresAt, nil
	}

	jwt, err := p.buildJWT(time.Now())
	if err != nil {
		return "", time.Time{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBaseURL, p.installationID),
		nil,
	)
	if err != nil {
		return "", time.Time{}, err
	}

	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion())
	req.Header.Set("User-Agent", "publiccode-crawler")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github app token response read failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("github app token request failed: %s", resp.Status)
	}

	var body tokenResponse
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return "", time.Time{}, fmt.Errorf("github app token response decode failed: %w", err)
	}

	if body.Token == "" {
		return "", time.Time{}, errors.New("github app token response missing token")
	}

	expiresAt, err := parseTime(body.ExpiresAt)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github app token response invalid expires_at: %w", err)
	}

	p.accessTok = body.Token
	p.expiresAt = expiresAt

	return p.accessTok, p.expiresAt, nil
}

func (t *tokenSource) Token() (*oauth2.Token, error) {
	token, expiresAt, err := t.provider.Token(t.ctx)
	if err != nil {
		return nil, err
	}

	return &oauth2.Token{
		AccessToken: token,
		TokenType:   "Bearer",
		Expiry:      expiresAt,
	}, nil
}

func (p *TokenProvider) buildJWT(now time.Time) (string, error) {
	claims := map[string]interface{}{
		"iat": now.Add(-jwtIssuedAtSkew).Unix(),
		"exp": now.Add(jwtExpiry).Unix(),
		"iss": p.appID,
	}

	return signJWT(claims, p.privateKey)
}

func signJWT(claims map[string]interface{}, privateKey *rsa.PrivateKey) (string, error) {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}

	encodedHeader, err := encodeJWTPart(header)
	if err != nil {
		return "", err
	}

	encodedPayload, err := encodeJWTPart(claims)
	if err != nil {
		return "", err
	}

	signingInput := encodedHeader + "." + encodedPayload
	hash := sha256.Sum256([]byte(signingInput))

	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("jwt signing failed: %w", err)
	}

	encodedSig := base64.RawURLEncoding.EncodeToString(signature)

	return signingInput + "." + encodedSig, nil
}

func encodeJWTPart(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(data), nil
}

// parsePrivateKey parses RSA PEM in PKCS1/PKCS8 form.
func parsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	secret := strings.ReplaceAll(strings.TrimSpace(raw), "\\n", "\n")
	block, _ := pem.Decode([]byte(secret))

	if block == nil {
		return nil, errors.New("GIT_OAUTH_SECRET is not valid PEM data")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid RSA private key: %w", err)
		}

		return key, nil
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid PKCS8 private key: %w", err)
		}

		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("GIT_OAUTH_SECRET is not an RSA private key")
		}

		return key, nil
	default:
		return nil, fmt.Errorf("unsupported private key type %q", block.Type)
	}
}

// parseTime accepts RFC3339 timestamps from the API.
func parseTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, errors.New("empty time")
	}

	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}

	return time.Parse(time.RFC3339Nano, raw)
}

// githubAPIVersion returns the API version header value.
func githubAPIVersion() string {
	value := strings.TrimSpace(os.Getenv("GITHUB_API_VERSION"))
	if value == "" {
		return githubAPIVersionDefault
	}

	return value
}
