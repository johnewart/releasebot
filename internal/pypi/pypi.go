package pypi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://pypi.org"

// Check returns true if the package exists on PyPI. If version is non-empty,
// returns true only when that specific version is published (200 from
// /pypi/<name>/<version>/json). Otherwise checks /pypi/<name>/json.
// Returns false and nil on 404.
func Check(ctx context.Context, name, version string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("package name is required")
	}
	base, err := url.Parse(defaultBaseURL)
	if err != nil {
		return false, err
	}
	var u *url.URL
	if version != "" {
		version = strings.TrimSpace(version)
		u = base.JoinPath("pypi", name, version, "json")
	} else {
		u = base.JoinPath("pypi", name, "json")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("pypi returned %d for %s", resp.StatusCode, u.Redacted())
	}
}

// WaitOptions configures Wait behavior.
type WaitOptions struct {
	// Timeout is the maximum time to wait for the package/version to appear (default 5m).
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

// Wait polls PyPI until the package (and optionally version) exists or the context/timeout is exceeded.
// Returns nil when the package is available; returns an error on timeout or other failure.
func Wait(ctx context.Context, name, version string, opts WaitOptions) error {
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
		ok, err := Check(ctx, name, version)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			ref := name
			if version != "" {
				ref = name + "==" + version
			}
			return fmt.Errorf("package %s not available on PyPI after %v", ref, opts.Timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			continue
		}
	}
}
