package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	cf "github.com/FutureGadgetResearch/site-generator-backend/internal/cloudflare"
	gh "github.com/FutureGadgetResearch/site-generator-backend/internal/github"
)

// Options holds the parameters for generating a new site repo.
type Options struct {
	Token      string
	Org        string
	Template   string
	SiteName   string
	Type       string
	Data       json.RawMessage
	BaseDomain string

	// Optional image to write to static/images/primary.{ext}.
	ImageData []byte
	ImageExt  string // e.g. ".jpg", ".png", ".webp"

	// Cloudflare DNS settings — both must be set to create a CNAME record.
	CFAPIToken string
	CFZoneID   string
}

// GenerateSite clones a template repo, injects data as data/{type}.json,
// re-initialises git history, creates a new GitHub repo, and pushes. It
// returns the HTML URL of the newly created repository.
func GenerateSite(ctx context.Context, opts Options) (string, error) {
	tmpDir, err := os.MkdirTemp("", "site-generator-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	auth := &githttp.BasicAuth{
		Username: "x-access-token",
		Password: opts.Token,
	}

	// 1. Clone the template repo
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", opts.Org, opts.Template)
	log.Printf("cloning template repo: %s", cloneURL)

	_, err = git.PlainClone(tmpDir, false, &git.CloneOptions{
		URL:  cloneURL,
		Auth: auth,
	})
	if err != nil {
		return "", fmt.Errorf("cloning template repo: %w", err)
	}

	// 2. Inject data — write data/{type}.json
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("creating data directory: %w", err)
	}

	prettyData, err := json.MarshalIndent(opts.Data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling data: %w", err)
	}

	dataFile := filepath.Join(dataDir, opts.Type+".json")
	if err := os.WriteFile(dataFile, prettyData, 0644); err != nil {
		return "", fmt.Errorf("writing data file: %w", err)
	}

	// 2b. Write the uploaded image to static/images/primary.{ext} if provided
	if len(opts.ImageData) > 0 {
		imgDir := filepath.Join(tmpDir, "static", "images")
		if err := os.MkdirAll(imgDir, 0755); err != nil {
			return "", fmt.Errorf("creating static/images directory: %w", err)
		}
		imgFile := filepath.Join(imgDir, "primary"+opts.ImageExt)
		if err := os.WriteFile(imgFile, opts.ImageData, 0644); err != nil {
			return "", fmt.Errorf("writing image file: %w", err)
		}
	}

	// 3. Remove .git and re-init for a clean history
	if err := os.RemoveAll(filepath.Join(tmpDir, ".git")); err != nil {
		return "", fmt.Errorf("removing .git: %w", err)
	}

	repo, err := git.PlainInitWithOptions(tmpDir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{
			DefaultBranch: plumbing.NewBranchReferenceName("main"),
		},
	})
	if err != nil {
		return "", fmt.Errorf("initializing new repo: %w", err)
	}

	// 4. Check if repo already exists, then create new GitHub repo
	exists, err := gh.RepoExists(ctx, opts.Token, opts.Org, opts.SiteName)
	if err != nil {
		return "", fmt.Errorf("checking repo existence: %w", err)
	}
	if exists {
		return "", fmt.Errorf("repository %s/%s already exists", opts.Org, opts.SiteName)
	}

	result, err := gh.CreateRepo(ctx, opts.Token, opts.Org, opts.SiteName)
	if err != nil {
		return "", err
	}

	// 5. Add all files, commit, and push
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("getting worktree: %w", err)
	}

	if _, err := wt.Add("."); err != nil {
		return "", fmt.Errorf("adding files: %w", err)
	}

	_, err = wt.Commit("Initial commit from template "+opts.Template, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Site Generator",
			Email: "generator@futuregatgetresearch.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{result.CloneURL},
	})
	if err != nil {
		return "", fmt.Errorf("adding remote: %w", err)
	}

	if err := repo.Push(&git.PushOptions{Auth: auth}); err != nil {
		return "", fmt.Errorf("pushing to new repo: %w", err)
	}

	// 6. Enable GitHub Pages with GitHub Actions as the build source
	if err := gh.EnablePages(ctx, opts.Token, opts.Org, opts.SiteName); err != nil {
		return "", fmt.Errorf("enabling GitHub Pages: %w", err)
	}

	// 7. Set custom domain for GitHub Pages
	log.Printf("PAGES_BASE_DOMAIN=%q", opts.BaseDomain)
	if opts.BaseDomain != "" {
		domain := opts.SiteName + "." + opts.BaseDomain
		if err := gh.SetCustomDomain(ctx, opts.Token, opts.Org, opts.SiteName, domain); err != nil {
			return "", fmt.Errorf("setting custom domain: %w", err)
		}
		log.Printf("custom domain set: %s", domain)
	}

	// 8. Create Cloudflare CNAME record
	if opts.CFAPIToken != "" && opts.CFZoneID != "" && opts.BaseDomain != "" {
		target := opts.Org + ".github.io"
		if err := cf.EnsureCNAME(ctx, opts.CFAPIToken, opts.CFZoneID, opts.SiteName, opts.BaseDomain, target); err != nil {
			return "", fmt.Errorf("creating Cloudflare CNAME: %w", err)
		}
		log.Printf("Cloudflare CNAME created: %s.%s -> %s", opts.SiteName, opts.BaseDomain, target)
	}

	log.Printf("successfully created site repo: %s", result.HTMLURL)
	return result.HTMLURL, nil
}
