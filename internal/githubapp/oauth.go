package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// UserAccessToken is the JSON response from GitHub's OAuth access_token endpoint.
type UserAccessToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// ExchangeUserAccessToken exchanges an authorization code for a user access token (GitHub App OAuth).
func ExchangeUserAccessToken(ctx context.Context, clientID, clientSecret, code string) (*UserAccessToken, error) {
	if clientID == "" || clientSecret == "" || code == "" {
		return nil, fmt.Errorf("client_id, client_secret, and code are required")
	}
	body, err := json.Marshal(map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github oauth token endpoint: %s: %s", resp.Status, bytes.TrimSpace(data))
	}
	var out UserAccessToken
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode oauth response: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("oauth response missing access_token")
	}
	return &out, nil
}
