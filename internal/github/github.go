package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v60/github"
)

// RepoResult holds the URLs returned after creating a GitHub repo.
type RepoResult struct {
	CloneURL string
	HTMLURL  string
}

// GetInstallationToken authenticates as a GitHub App installation and returns
// an access token. It reads GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, and
// GITHUB_APP_PRIVATE_KEY from the environment.
func GetInstallationToken() (string, error) {
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		return "", fmt.Errorf("GITHUB_APP_ID not configured")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}

	installationIDStr := os.Getenv("GITHUB_APP_INSTALLATION_ID")
	if installationIDStr == "" {
		return "", fmt.Errorf("GITHUB_APP_INSTALLATION_ID not configured")
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_APP_INSTALLATION_ID: %w", err)
	}

	privateKey := os.Getenv("GITHUB_APP_PRIVATE_KEY")
	if privateKey == "" {
		return "", fmt.Errorf("GITHUB_APP_PRIVATE_KEY not configured")
	}

	itr, err := ghinstallation.New(http.DefaultTransport, appID, installationID, []byte(privateKey))
	if err != nil {
		return "", fmt.Errorf("creating GitHub App transport: %w", err)
	}

	token, err := itr.Token(context.Background())
	if err != nil {
		return "", fmt.Errorf("generating installation token: %w", err)
	}

	return token, nil
}

// RepoExists checks whether a repository with the given name already exists
// under the given org.
func RepoExists(ctx context.Context, token, org, name string) (bool, error) {
	client := github.NewClient(nil).WithAuthToken(token)

	_, resp, err := client.Repositories.Get(ctx, org, name)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("checking repo existence: %w", err)
	}
	return true, nil
}

// CreateRepo creates a new GitHub repository under the given org and returns
// its clone and HTML URLs.
func CreateRepo(ctx context.Context, token, org, name string) (*RepoResult, error) {
	client := github.NewClient(nil).WithAuthToken(token)

	repo, _, err := client.Repositories.Create(ctx, org, &github.Repository{
		Name:    github.String(name),
		Private: github.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("creating GitHub repo: %w", err)
	}

	return &RepoResult{
		CloneURL: repo.GetCloneURL(),
		HTMLURL:  repo.GetHTMLURL(),
	}, nil
}

// DeleteRepo deletes a GitHub repository under the given org.
func DeleteRepo(ctx context.Context, token, org, name string) error {
	client := github.NewClient(nil).WithAuthToken(token)

	_, err := client.Repositories.Delete(ctx, org, name)
	if err != nil {
		return fmt.Errorf("deleting GitHub repo: %w", err)
	}
	return nil
}

// EnablePages enables GitHub Pages on a repository with GitHub Actions as the
// build source.
func EnablePages(ctx context.Context, token, org, name string) error {
	client := github.NewClient(nil).WithAuthToken(token)

	_, _, err := client.Repositories.EnablePages(ctx, org, name, &github.Pages{
		BuildType: github.String("workflow"),
	})
	if err != nil {
		return fmt.Errorf("enabling GitHub Pages: %w", err)
	}
	return nil
}

// SetCustomDomain configures a custom domain (CNAME) for GitHub Pages on the
// given repository. It skips the update if the domain is already configured.
func SetCustomDomain(ctx context.Context, token, org, repo, domain string) error {
	client := github.NewClient(nil).WithAuthToken(token)

	// Check if the custom domain is already set.
	pages, _, err := client.Repositories.GetPagesInfo(ctx, org, repo)
	if err == nil && pages != nil && pages.GetCNAME() == domain {
		return nil
	}

	_, err = client.Repositories.UpdatePages(ctx, org, repo, &github.PagesUpdate{
		CNAME: github.String(domain),
	})
	if err != nil {
		return fmt.Errorf("setting custom domain: %w", err)
	}
	return nil
}
