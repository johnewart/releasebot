package dockerhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultRegistryHost = "registry-1.docker.io"
	defaultAuthHost     = "https://auth.docker.io"
	defaultService      = "registry.docker.io"
)

// Check returns true if the image (e.g. "nginx:latest", "myorg/myimage:v1.0") exists on Docker Hub.
// It uses the Registry API v2 HEAD manifest endpoint. Returns false and nil on 404.
func Check(ctx context.Context, image string) (bool, error) {
	repo, ref, err := parseImageRef(image)
	if err != nil {
		return false, err
	}
	token, err := getToken(ctx, repo)
	if err != nil {
		return false, fmt.Errorf("docker hub auth: %w", err)
	}
	ok, err := headManifest(ctx, repo, ref, token)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// WaitOptions configures Wait behavior.
type WaitOptions struct {
	// Timeout is the maximum time to wait for the image to appear (default 5m).
	Timeout time.Duration
	// Interval is how often to poll (default 5s).
	Interval time.Duration
}

// DefaultWaitOptions returns defaults: 5m timeout, 5s interval.
func DefaultWaitOptions() WaitOptions {
	return WaitOptions{
		Timeout:  5 * time.Minute,
		Interval: 5 * time.Second,
	}
}

// Wait polls Docker Hub until the image exists or the context/timeout is exceeded.
// Returns nil when the image is available; returns an error on timeout or other failure.
func Wait(ctx context.Context, image string, opts WaitOptions) error {
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.Interval == 0 {
		opts.Interval = 5 * time.Second
	}
	deadline := time.Now().Add(opts.Timeout)
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		ok, err := Check(ctx, image)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("image %s not available on Docker Hub after %v", image, opts.Timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			continue
		}
	}
}

func parseImageRef(image string) (repo, ref string, err error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", "", fmt.Errorf("empty image reference")
	}
	// Strip docker.io prefix if present
	image = strings.TrimPrefix(image, "docker.io/")
	// ref is tag or digest (after last colon that is part of tag/digest)
	lastColon := strings.LastIndex(image, ":")
	if lastColon == -1 {
		// no tag => latest
		return normalizeRepo(image), "latest", nil
	}
	refPart := image[lastColon+1:]
	// digest contains @ or starts with sha256:
	if strings.HasPrefix(refPart, "sha256:") || strings.Contains(refPart, "@") {
		repo = image[:lastColon]
		ref = refPart
		return normalizeRepo(repo), ref, nil
	}
	// could be tag or port in host (e.g. host:5000/repo:tag). For Docker Hub we don't have port, so it's tag.
	repo = image[:lastColon]
	ref = refPart
	return normalizeRepo(repo), ref, nil
}

// normalizeRepo: "nginx" -> "library/nginx", "myorg/myimage" -> "myorg/myimage"
func normalizeRepo(repo string) string {
	if repo == "" {
		return repo
	}
	if !strings.Contains(repo, "/") {
		return "library/" + repo
	}
	return repo
}

type tokenResponse struct {
	Token string `json:"token"`
}

func getToken(ctx context.Context, repo string) (string, error) {
	u := defaultAuthHost + "/token?service=" + url.QueryEscape(defaultService) +
		"&scope=repository:" + url.QueryEscape(repo) + ":pull"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth returned %d", resp.StatusCode)
	}
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	if tr.Token == "" {
		return "", fmt.Errorf("empty token in auth response")
	}
	return tr.Token, nil
}

func headManifest(ctx context.Context, repo, ref, token string) (bool, error) {
	u := "https://" + defaultRegistryHost + "/v2/" + repo + "/manifests/" + ref
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// Prefer v2 manifest so we don't pull a huge body
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound, http.StatusUnauthorized:
		// 404 = unknown manifest; 401 = no pull access (private or missing)
		return false, nil
	default:
		return false, fmt.Errorf("manifest HEAD returned %d", resp.StatusCode)
	}
}
