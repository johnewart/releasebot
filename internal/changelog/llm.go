package changelog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/ollama/ollama/api"
	"github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"
)

const (
	ProviderOpenAI    = "openai"
	ProviderOllama    = "ollama"
	ProviderAnthropic = "anthropic"

	defaultOllamaHost     = "http://localhost:11434"
	defaultOllamaModel    = "llama3.2"
	defaultOpenAIModel    = "gpt-4o-mini"
	defaultAnthropicModel = "claude-sonnet-4-5-20250929"

	maxLLMRetries  = 3
	initialBackoff = 1 * time.Second
	maxBackoff     = 30 * time.Second
)

// isRecoverableError reports whether the error is transient and worth retrying (e.g. 5xx, 429, timeout).
func isRecoverableError(err error) bool {
	if err == nil {
		return false
	}
	var openaiErr *openai.Error
	if errors.As(err, &openaiErr) {
		return statusCodeRetryable(openaiErr.StatusCode)
	}
	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) {
		return statusCodeRetryable(anthropicErr.StatusCode)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isRecoverableError(urlErr.Err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return false
}

func statusCodeRetryable(code int) bool {
	return code == http.StatusTooManyRequests || (code >= 500 && code < 600)
}

// retryWithBackoff runs fn up to maxAttempts times with exponential backoff on recoverable errors.
func retryWithBackoff(ctx context.Context, maxAttempts int, fn func() (string, error)) (string, error) {
	var lastErr error
	backoff := initialBackoff
	for attempt := 0; attempt < maxAttempts; attempt++ {
		out, err := fn()
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isRecoverableError(err) || attempt == maxAttempts-1 {
			return "", err
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	return "", lastErr
}

// Generator produces a changelog section from version, format, and entries.
// When summarize_per_pr is on: SummarizePR is called once per PR (result cached as JSON); the final
// changelog is built from that JSON (template). When off: GenerateChangelogSection is called once with all PRs.
type Generator interface {
	GenerateChangelogSection(ctx context.Context, version, format string, entries interface{}) (string, error)
	// SummarizePR returns structured change info (change_type, description, pr_id) as JSON; parse with ParsePRChangeJSON.
	// metadata is title/body/author; diff is optional (unified diff when include_diff is true).
	SummarizePR(ctx context.Context, metadata, diff string, prID int) (string, error)
}

// LLM is the OpenAI-backed generator (implements Generator).
type LLM struct {
	client *openai.Client
	model  string
}

// NewLLM creates a Generator for the given provider ("openai", "ollama", or "anthropic").
// OpenAI: OPENAI_API_KEY required; optional OPENAI_BASE_URL.
// Ollama: uses the official Ollama Go SDK and POST /api/generate; OLLAMA_HOST for base URL.
// Anthropic: ANTHROPIC_API_KEY required; optional base_url for custom endpoint.
func NewLLM(provider, model, baseURL string) (Generator, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = ProviderOpenAI
	}

	switch provider {
	case ProviderOllama:
		return newOllamaGenerator(model, baseURL)
	case ProviderOpenAI:
		return newOpenAIGenerator(model, baseURL)
	case ProviderAnthropic:
		return newAnthropicGenerator(model, baseURL)
	default:
		return nil, fmt.Errorf("unknown LLM provider %q (use %q, %q, or %q)", provider, ProviderOpenAI, ProviderOllama, ProviderAnthropic)
	}
}

// newOllamaGenerator uses the Ollama Go SDK and the generate endpoint.
func newOllamaGenerator(model, baseURL string) (Generator, error) {
	if model == "" {
		model = defaultOllamaModel
	}
	host := baseURL
	if host == "" {
		host = os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = defaultOllamaHost
		}
	}
	if !strings.HasPrefix(host, "http") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("ollama host URL: %w", err)
	}
	client := api.NewClient(u, http.DefaultClient)
	return &ollamaGenerator{client: client, model: model}, nil
}

type ollamaGenerator struct {
	client *api.Client
	model  string
}

func (o *ollamaGenerator) GenerateChangelogSection(ctx context.Context, version, format string, entries interface{}) (string, error) {
	prompt := buildPrompt(version, format, entries)
	system := "You are a release notes writer. Output only the requested changelog section in valid Markdown. Do not add extra commentary or headers other than the version heading."
	stream := false
	req := &api.GenerateRequest{
		Model:  o.model,
		Prompt: prompt,
		System: system,
		Stream: &stream,
	}
	out, err := retryWithBackoff(ctx, maxLLMRetries, func() (string, error) {
		var full strings.Builder
		err := o.client.Generate(ctx, req, func(resp api.GenerateResponse) error {
			full.WriteString(resp.Response)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("ollama generate: %w", err)
		}
		s := strings.TrimSpace(full.String())
		if s == "" {
			return "", fmt.Errorf("ollama returned empty response")
		}
		return s, nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

func (o *ollamaGenerator) SummarizePR(ctx context.Context, metadata, diff string, prID int) (string, error) {
	prompt := buildSummarizePRPrompt(metadata, diff, prID)
	system := summarizePRSystemPrompt
	stream := false
	req := &api.GenerateRequest{
		Model:  o.model,
		Prompt: prompt,
		System: system,
		Stream: &stream,
	}
	return retryWithBackoff(ctx, maxLLMRetries, func() (string, error) {
		var full strings.Builder
		err := o.client.Generate(ctx, req, func(resp api.GenerateResponse) error {
			full.WriteString(resp.Response)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("ollama summarize PR: %w", err)
		}
		return strings.TrimSpace(full.String()), nil
	})
}

// newAnthropicGenerator uses the Anthropic Messages API (anthropic-sdk-go).
func newAnthropicGenerator(model, baseURL string) (Generator, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set (required for provider anthropic)")
	}
	opts := []anthropicoption.RequestOption{anthropicoption.WithAPIKey(key)}
	if baseURL != "" {
		opts = append(opts, anthropicoption.WithBaseURL(baseURL))
	}
	if model == "" {
		model = defaultAnthropicModel
	}
	client := anthropic.NewClient(opts...)
	return &anthropicGenerator{client: client, model: model}, nil
}

type anthropicGenerator struct {
	client anthropic.Client
	model  string
}

func (a *anthropicGenerator) GenerateChangelogSection(ctx context.Context, version, format string, entries interface{}) (string, error) {
	prompt := buildPrompt(version, format, entries)
	system := "You are a release notes writer. Output only the requested changelog section in valid Markdown. Do not add extra commentary or headers other than the version heading."
	out, err := retryWithBackoff(ctx, maxLLMRetries, func() (string, error) {
		msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(a.model),
			MaxTokens: 4096,
			System:    []anthropic.TextBlockParam{{Text: system}},
			Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
		})
		if err != nil {
			return "", fmt.Errorf("anthropic messages: %w", err)
		}
		s := extractAnthropicText(msg.Content)
		if s == "" {
			return "", fmt.Errorf("anthropic returned empty response")
		}
		return s, nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

func (a *anthropicGenerator) SummarizePR(ctx context.Context, metadata, diff string, prID int) (string, error) {
	prompt := buildSummarizePRPrompt(metadata, diff, prID)
	return retryWithBackoff(ctx, maxLLMRetries, func() (string, error) {
		msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(a.model),
			MaxTokens: 1024,
			System:    []anthropic.TextBlockParam{{Text: summarizePRSystemPrompt}},
			Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
		})
		if err != nil {
			return "", fmt.Errorf("anthropic summarize PR: %w", err)
		}
		return strings.TrimSpace(extractAnthropicText(msg.Content)), nil
	})
}

// extractAnthropicText concatenates text from all text content blocks in the message.
func extractAnthropicText(content []anthropic.ContentBlockUnion) string {
	var b strings.Builder
	for _, block := range content {
		if block.Type == "text" {
			t := block.AsText()
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// newOpenAIGenerator uses the OpenAI API (openai-go client).
func newOpenAIGenerator(model, baseURL string) (*LLM, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set (required for provider openai)")
	}
	opts := []openaioption.RequestOption{openaioption.WithAPIKey(key)}
	if baseURL == "" {
		baseURL = os.Getenv("OPENAI_BASE_URL")
	}
	if baseURL != "" {
		opts = append(opts, openaioption.WithBaseURL(baseURL))
	}
	if model == "" {
		model = defaultOpenAIModel
	}
	client := openai.NewClient(opts...)
	return &LLM{client: client, model: model}, nil
}

// GenerateChangelogSection implements Generator for OpenAI.
func (l *LLM) GenerateChangelogSection(ctx context.Context, version, format string, entries interface{}) (string, error) {
	prompt := buildPrompt(version, format, entries)
	out, err := retryWithBackoff(ctx, maxLLMRetries, func() (string, error) {
		resp, err := l.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model: openai.F(openai.ChatModel(l.model)),
			Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage("You are a release notes writer. Output only the requested changelog section in valid Markdown. Do not add extra commentary or headers other than the version heading."),
				openai.UserMessage(prompt),
			}),
		})
		if err != nil {
			return "", fmt.Errorf("chat completion: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("no choices in response")
		}
		content := resp.Choices[0].Message.Content
		if content == "" {
			return "", fmt.Errorf("empty content")
		}
		return content, nil
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

func (l *LLM) SummarizePR(ctx context.Context, metadata, diff string, prID int) (string, error) {
	prompt := buildSummarizePRPrompt(metadata, diff, prID)
	return retryWithBackoff(ctx, maxLLMRetries, func() (string, error) {
		resp, err := l.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model: openai.F(openai.ChatModel(l.model)),
			Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(summarizePRSystemPrompt),
				openai.UserMessage(prompt),
			}),
		})
		if err != nil {
			return "", fmt.Errorf("summarize PR: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("no choices in response")
		}
		content := strings.TrimSpace(resp.Choices[0].Message.Content)
		return content, nil
	})
}

const summarizePRSystemPrompt = `You are a release notes classifier. Output only valid JSON, no other text.
Use this exact JSON format: {"change_type": "<type>", "description": "<one line description>", "pr_id": <number>}
change_type must be exactly one of: Added, Changed, Developer Experience, Deprecated, Docs, Removed, Fixed, Security.
description should be a single concise line describing what this PR changed (e.g. "Add retry logic for flaky tests").

Example output:
{"change_type": "Added", "description": "Add retry logic for flaky tests", "pr_id": 12345}

Example input for PR #12345:
Pull request #12345 metadata:
Title: Add retry logic for flaky tests
Author: @johndoe
Merged: 2026-01-01

Unified diff:
...
`

func buildSummarizePRPrompt(metadata, diff string, prID int) string {
	out := fmt.Sprintf("Pull request #%d metadata:\n%s", prID, metadata)
	if diff != "" {
		const maxDiffLen = 12000
		if len(diff) > maxDiffLen {
			diff = diff[:maxDiffLen] + "\n\n... (diff truncated)"
		}
		out += "\n\nUnified diff:\n" + diff
	}
	out += fmt.Sprintf("\n\nOutput only a single JSON object with change_type, description, and pr_id (%d).", prID)
	return out
}

func buildPrompt(version, format string, entries interface{}) string {
	var body string
	switch v := entries.(type) {
	case string:
		body = v
	default:
		body = fmt.Sprintf("%v", v)
	}
	return fmt.Sprintf(`Generate a changelog section for version %s.

Use the following template as the structure for your output, not an exact template; if the template has 
an <instructions> tag, follow the instructions in the tag as instructions for that specific template.

Structure template:
%s

Input data to turn into changelog entries:
%s
`, version, format, body)
}
