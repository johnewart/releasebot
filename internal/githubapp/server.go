package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-github/v60/github"
)

// Server is an HTTP handler for GitHub webhooks and optional GitHub App user OAuth.
type Server struct {
	settings          Settings
	mux               *http.ServeMux
	states            *oauthStateStore
	webhookPath       string
	oauthLoginPath    string
	oauthCallbackPath string
}

// NewServer builds routes for health, webhooks, and OAuth (when configured).
func NewServer(settings Settings, webhookPath, oauthLoginPath, oauthCallbackPath string) *Server {
	if webhookPath == "" {
		webhookPath = "/github/webhook"
	}
	if oauthLoginPath == "" {
		oauthLoginPath = "/oauth/github/login"
	}
	if oauthCallbackPath == "" {
		oauthCallbackPath = "/oauth/github/callback"
	}
	s := &Server{
		settings: settings,
		mux:      http.NewServeMux(),
		states:   newOAuthStateStore(),
	}
	wp := ensureLeadingSlash(trimTrailingSlash(webhookPath))
	lp := ensureLeadingSlash(trimTrailingSlash(oauthLoginPath))
	cp := ensureLeadingSlash(trimTrailingSlash(oauthCallbackPath))
	s.webhookPath, s.oauthLoginPath, s.oauthCallbackPath = wp, lp, cp

	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc(wp, s.handleWebhook)
	s.mux.HandleFunc(lp, s.handleOAuthLogin)
	s.mux.HandleFunc(cp, s.handleOAuthCallback)
	return s
}

func trimTrailingSlash(p string) string {
	return strings.TrimSuffix(p, "/")
}

func ensureLeadingSlash(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) callbackURL() string {
	return s.settings.PublicBaseURL + s.oauthCallbackPath
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret := []byte(s.settings.WebhookSecret)
	if len(secret) == 0 {
		log.Println("githubapp: warning: RELEASEBOT_GITHUB_WEBHOOK_SECRET is empty; webhook signatures are not verified")
	}
	payload, err := github.ValidatePayload(r, secret)
	if err != nil {
		log.Printf("githubapp: webhook: invalid payload: %v", err)
		http.Error(w, "invalid payload", http.StatusUnauthorized)
		return
	}
	eventType := github.WebHookType(r)
	delivery := github.DeliveryID(r)
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		log.Printf("githubapp: webhook: parse %q: %v", eventType, err)
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}
	s.logAndDispatch(eventType, delivery, event)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) logAndDispatch(eventType, delivery string, event interface{}) {
	switch e := event.(type) {
	case *github.PingEvent:
		log.Printf("githubapp: webhook ping hook_id=%v delivery=%s", e.GetHookID(), delivery)
	case *github.InstallationEvent:
		log.Printf("githubapp: installation action=%s id=%d delivery=%s",
			e.GetAction(), e.GetInstallation().GetID(), delivery)
	case *github.InstallationRepositoriesEvent:
		log.Printf("githubapp: installation_repositories action=%s installation=%d delivery=%s",
			e.GetAction(), e.GetInstallation().GetID(), delivery)
	case *github.PullRequestEvent:
		repo := e.GetRepo().GetFullName()
		log.Printf("githubapp: pull_request action=%s repo=%s number=%d delivery=%s",
			e.GetAction(), repo, e.GetNumber(), delivery)
	case *github.PushEvent:
		log.Printf("githubapp: push repo=%s ref=%s delivery=%s", e.GetRepo().GetFullName(), e.GetRef(), delivery)
	case *github.ReleaseEvent:
		log.Printf("githubapp: release action=%s repo=%s tag=%s delivery=%s",
			e.GetAction(), e.GetRepo().GetFullName(), e.GetRelease().GetTagName(), delivery)
	default:
		log.Printf("githubapp: webhook event=%s type=%T delivery=%s", eventType, event, delivery)
	}
}

func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.settings.OAuthClientID == "" {
		http.Error(w, "OAuth is not configured (set RELEASEBOT_GITHUB_APP_CLIENT_ID)", http.StatusServiceUnavailable)
		return
	}
	if s.settings.PublicBaseURL == "" {
		http.Error(w, "RELEASEBOT_PUBLIC_URL must be set to the public origin of this server for OAuth", http.StatusServiceUnavailable)
		return
	}
	state, err := s.states.issue()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	q := url.Values{}
	q.Set("client_id", s.settings.OAuthClientID)
	q.Set("redirect_uri", s.callbackURL())
	q.Set("state", state)
	if s.settings.OAuthScopes != "" {
		q.Set("scope", s.settings.OAuthScopes)
	}
	u := "https://github.com/login/oauth/authorize?" + q.Encode()
	http.Redirect(w, r, u, http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.settings.OAuthClientSecret == "" {
		http.Error(w, "OAuth is not configured (set RELEASEBOT_GITHUB_APP_CLIENT_SECRET)", http.StatusServiceUnavailable)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		http.Error(w, fmt.Sprintf("github error: %s %s", errParam, desc), http.StatusBadRequest)
		return
	}
	if !s.states.consume(state) {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	tok, err := ExchangeUserAccessToken(ctx, s.settings.OAuthClientID, s.settings.OAuthClientSecret, code)
	if err != nil {
		log.Printf("githubapp: oauth token exchange: %v", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	login := ""
	if tok.AccessToken != "" {
		gh := github.NewClient(nil).WithAuthToken(tok.AccessToken)
		user, _, err := gh.Users.Get(ctx, "")
		if err == nil && user != nil {
			login = user.GetLogin()
			log.Printf("githubapp: oauth success user=%s", login)
		} else if err != nil {
			log.Printf("githubapp: oauth: could not fetch user: %v", err)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]interface{}{
		"ok":           true,
		"login":        login,
		"access_token": tok.AccessToken,
		"token_type":   tok.TokenType,
		"scope":        tok.Scope,
	})
}
