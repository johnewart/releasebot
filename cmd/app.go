package cmd

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/johnewart/releasebot/internal/githubapp"
	"github.com/spf13/cobra"
)

var (
	appAddr            string
	appPublicURL       string
	appWebhookPath     string
	appOAuthLoginPath  string
	appOAuthCallback   string
	appOAuthScopes     string
)

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "Run releasebot as a GitHub App HTTP server (webhooks + optional OAuth)",
	Long: `Listen for GitHub webhooks and optionally complete GitHub App user OAuth flows.

Configure with environment variables (see README). Typical routes:
  GET  /healthz
  POST /<webhook-path>
  GET  /<oauth-login-path>   — redirect to GitHub (when OAuth env is set)
  GET  /<oauth-callback-path> — exchange code, returns JSON with access_token`,
}

var appServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	RunE:  runAppServe,
}

func init() {
	rootCmd.AddCommand(appCmd)
	appCmd.AddCommand(appServeCmd)
	appServeCmd.Flags().StringVar(&appAddr, "addr", ":8080", "listen address (e.g. :8080)")
	appServeCmd.Flags().StringVar(&appPublicURL, "public-url", "", "override RELEASEBOT_PUBLIC_URL (OAuth callback base)")
	appServeCmd.Flags().StringVar(&appWebhookPath, "webhook-path", "/github/webhook", "webhook URL path")
	appServeCmd.Flags().StringVar(&appOAuthLoginPath, "oauth-login-path", "/oauth/github/login", "browser entry for OAuth")
	appServeCmd.Flags().StringVar(&appOAuthCallback, "oauth-callback-path", "/oauth/github/callback", "must match GitHub App callback URL")
	appServeCmd.Flags().StringVar(&appOAuthScopes, "oauth-scopes", "", "override RELEASEBOT_GITHUB_OAUTH_SCOPES")
}

func runAppServe(cmd *cobra.Command, args []string) error {
	settings, err := githubapp.LoadSettingsFromEnv()
	if err != nil {
		return err
	}
	if appPublicURL != "" {
		settings.PublicBaseURL = strings.TrimSuffix(strings.TrimSpace(appPublicURL), "/")
	}
	if appOAuthScopes != "" {
		settings.OAuthScopes = strings.TrimSpace(appOAuthScopes)
	}
	if settings.WebhookSecret == "" {
		log.Println("warning: RELEASEBOT_GITHUB_WEBHOOK_SECRET is empty; GitHub can deliver unsigned verification in dev only")
	}

	srv := githubapp.NewServer(settings, appWebhookPath, appOAuthLoginPath, appOAuthCallback)
	log.Printf("releasebot app listening on %s (webhook %s)", appAddr, appWebhookPath)
	if settings.PublicBaseURL != "" && settings.OAuthClientID != "" {
		log.Printf("oauth login: %s%s", settings.PublicBaseURL, appOAuthLoginPath)
	}
	if err := http.ListenAndServe(appAddr, srv); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}
