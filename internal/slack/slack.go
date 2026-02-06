package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// NotifyRunComplete sends a message to the Slack channel configured by webhookURL.
// If webhookURL is empty, SLACK_WEBHOOK_URL env is used. No-op if both are empty.
// success is whether the run succeeded; detail is optional context (e.g. error message or "Changelog written to X").
func NotifyRunComplete(webhookURL string, success bool, detail string) error {
	if webhookURL == "" {
		webhookURL = os.Getenv("SLACK_WEBHOOK_URL")
	}
	if webhookURL == "" {
		return nil
	}
	var text string
	if success {
		text = "✅ Releasebot run completed successfully."
		if detail != "" {
			text += " " + detail
		}
	} else {
		text = "❌ Releasebot run failed."
		if detail != "" {
			text += " " + detail
		}
	}
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("slack marshal: %w", err)
	}
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %s", resp.Status)
	}
	return nil
}
