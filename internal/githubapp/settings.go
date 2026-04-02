package githubapp

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Settings holds GitHub App and OAuth configuration (typically from environment).
type Settings struct {
	AppID             int64
	PrivateKeyPEM     []byte
	WebhookSecret     string
	OAuthClientID     string
	OAuthClientSecret string
	PublicBaseURL     string
	OAuthScopes       string
}

// LoadSettingsFromEnv reads configuration from environment variables.
//
// RELEASEBOT_GITHUB_APP_ID — numeric app ID (optional; required for installation API clients).
// RELEASEBOT_GITHUB_APP_PRIVATE_KEY — PEM private key (literal; newlines as \n ok in some hosts).
// RELEASEBOT_GITHUB_APP_PRIVATE_KEY_PATH — path to PEM file (preferred on servers).
// RELEASEBOT_GITHUB_WEBHOOK_SECRET — webhook secret from the GitHub App settings (empty disables signature verification; not for production).
// RELEASEBOT_GITHUB_APP_CLIENT_ID, RELEASEBOT_GITHUB_APP_CLIENT_SECRET — GitHub App "OAuth credentials" for user-to-server OAuth.
// RELEASEBOT_PUBLIC_URL — public origin of this server (e.g. https://releasebot.example.com), no trailing slash; required for OAuth redirects.
// RELEASEBOT_GITHUB_OAUTH_SCOPES — optional space-separated scopes (GitHub App user auth often uses app-configured permissions; leave empty when unsure).
func LoadSettingsFromEnv() (Settings, error) {
	var s Settings
	if v := strings.TrimSpace(os.Getenv("RELEASEBOT_GITHUB_APP_ID")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Settings{}, fmt.Errorf("RELEASEBOT_GITHUB_APP_ID: %w", err)
		}
		s.AppID = id
	}

	var key []byte
	if p := strings.TrimSpace(os.Getenv("RELEASEBOT_GITHUB_APP_PRIVATE_KEY_PATH")); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return Settings{}, fmt.Errorf("read RELEASEBOT_GITHUB_APP_PRIVATE_KEY_PATH: %w", err)
		}
		key = b
	} else if raw := os.Getenv("RELEASEBOT_GITHUB_APP_PRIVATE_KEY"); strings.TrimSpace(raw) != "" {
		key = []byte(strings.ReplaceAll(raw, `\n`, "\n"))
	}
	s.PrivateKeyPEM = key

	s.WebhookSecret = os.Getenv("RELEASEBOT_GITHUB_WEBHOOK_SECRET")
	s.OAuthClientID = strings.TrimSpace(os.Getenv("RELEASEBOT_GITHUB_APP_CLIENT_ID"))
	s.OAuthClientSecret = strings.TrimSpace(os.Getenv("RELEASEBOT_GITHUB_APP_CLIENT_SECRET"))
	s.PublicBaseURL = strings.TrimSuffix(strings.TrimSpace(os.Getenv("RELEASEBOT_PUBLIC_URL")), "/")
	s.OAuthScopes = strings.TrimSpace(os.Getenv("RELEASEBOT_GITHUB_OAUTH_SCOPES"))

	if s.AppID != 0 && len(s.PrivateKeyPEM) == 0 {
		return Settings{}, fmt.Errorf("RELEASEBOT_GITHUB_APP_ID is set but no private key (RELEASEBOT_GITHUB_APP_PRIVATE_KEY or _PATH)")
	}
	if s.AppID == 0 && len(s.PrivateKeyPEM) > 0 {
		return Settings{}, fmt.Errorf("private key is set but RELEASEBOT_GITHUB_APP_ID is missing")
	}
	return s, nil
}
