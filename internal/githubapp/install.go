package githubapp

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v60/github"
)

// NewInstallationClient returns a GitHub API client that authenticates as the given app installation.
func NewInstallationClient(transport http.RoundTripper, appID, installationID int64, privateKeyPEM []byte) (*github.Client, error) {
	if appID == 0 || len(privateKeyPEM) == 0 {
		return nil, fmt.Errorf("app ID and private key are required for installation client")
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	itr, err := ghinstallation.New(transport, appID, installationID, privateKeyPEM)
	if err != nil {
		return nil, err
	}
	return github.NewClient(&http.Client{Transport: itr}), nil
}
