package function

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"

	cf "github.com/FutureGadgetResearch/site-generator-backend/internal/cloudflare"
	"github.com/FutureGadgetResearch/site-generator-backend/internal/generator"
	gh "github.com/FutureGadgetResearch/site-generator-backend/internal/github"
)

func init() {
	functions.HTTP("GenerateSite", HandleGenerateSite)
	functions.HTTP("RepoExists", HandleRepoExists)
	functions.HTTP("DeleteSite", HandleDeleteSite)
}

type Request struct {
	Template string          `json:"template"`
	SiteName string          `json:"site_name"`
	Type     string          `json:"type"`
	Data     json.RawMessage `json:"data"`
}

type Response struct {
	RepoURL string `json:"repo_url"`
}

type RepoExistsRequest struct {
	RepoName string `json:"repo_name"`
}

type RepoExistsResponse struct {
	Exists bool `json:"exists"`
}

func HandleRepoExists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RepoExistsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.RepoName == "" {
		http.Error(w, "repo_name is required", http.StatusBadRequest)
		return
	}

	token, err := gh.GetInstallationToken()
	if err != nil {
		log.Printf("error getting installation token: %v", err)
		http.Error(w, fmt.Sprintf("authentication failed: %v", err), http.StatusInternalServerError)
		return
	}

	org := os.Getenv("GITHUB_ORG")
	if org == "" {
		org = "FutureGadgetResearch"
	}

	exists, err := gh.RepoExists(r.Context(), token, org, req.RepoName)
	if err != nil {
		log.Printf("error checking repo existence: %v", err)
		http.Error(w, fmt.Sprintf("failed to check repo: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RepoExistsResponse{Exists: exists})
}

type DeleteSiteRequest struct {
	SiteName string `json:"site_name"`
}

func HandleDeleteSite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeleteSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.SiteName == "" {
		http.Error(w, "site_name is required", http.StatusBadRequest)
		return
	}

	token, err := gh.GetInstallationToken()
	if err != nil {
		log.Printf("error getting installation token: %v", err)
		http.Error(w, fmt.Sprintf("authentication failed: %v", err), http.StatusInternalServerError)
		return
	}

	org := os.Getenv("GITHUB_ORG")
	if org == "" {
		org = "FutureGadgetResearch"
	}

	if err := gh.DeleteRepo(r.Context(), token, org, req.SiteName); err != nil {
		log.Printf("error deleting repo: %v", err)
		http.Error(w, fmt.Sprintf("failed to delete repo: %v", err), http.StatusInternalServerError)
		return
	}

	// Clean up Cloudflare CNAME record if configured
	cfToken := os.Getenv("CLOUDFLARE_API_TOKEN")
	cfZoneID := os.Getenv("CLOUDFLARE_ZONE_ID")
	baseDomain := os.Getenv("PAGES_BASE_DOMAIN")
	if cfToken != "" && cfZoneID != "" && baseDomain != "" {
		if err := cf.DeleteCNAME(r.Context(), cfToken, cfZoneID, req.SiteName, baseDomain); err != nil {
			log.Printf("warning: failed to delete Cloudflare CNAME: %v", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// allowedImageExts is the set of accepted image file extensions.
var allowedImageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
}

func HandleGenerateSite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 32 MB max memory for multipart parsing
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, fmt.Sprintf("invalid multipart form: %v", err), http.StatusBadRequest)
		return
	}

	// Read metadata JSON from the "metadata" form field
	metadataStr := r.FormValue("metadata")
	if metadataStr == "" {
		http.Error(w, "metadata form field is required", http.StatusBadRequest)
		return
	}

	var req Request
	if err := json.Unmarshal([]byte(metadataStr), &req); err != nil {
		http.Error(w, fmt.Sprintf("invalid metadata JSON: %v", err), http.StatusBadRequest)
		return
	}

	if req.Template == "" || req.SiteName == "" || req.Type == "" || len(req.Data) == 0 {
		http.Error(w, "template, site_name, type, and data are required", http.StatusBadRequest)
		return
	}

	// Read the optional image file part
	var imageData []byte
	var imageExt string

	file, header, err := r.FormFile("image")
	if err == nil {
		defer file.Close()
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if !allowedImageExts[ext] {
			http.Error(w, fmt.Sprintf("unsupported image extension %q; allowed: .jpg, .jpeg, .png, .webp", ext), http.StatusBadRequest)
			return
		}
		imageData, err = io.ReadAll(file)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read image: %v", err), http.StatusBadRequest)
			return
		}
		imageExt = ext
	}

	token, err := gh.GetInstallationToken()
	if err != nil {
		log.Printf("error getting installation token: %v", err)
		http.Error(w, fmt.Sprintf("authentication failed: %v", err), http.StatusInternalServerError)
		return
	}

	org := os.Getenv("GITHUB_ORG")
	if org == "" {
		org = "FutureGadgetResearch"
	}

	repoURL, err := generator.GenerateSite(r.Context(), generator.Options{
		Token:      token,
		Org:        org,
		Template:   req.Template,
		SiteName:   req.SiteName,
		Type:       req.Type,
		Data:       req.Data,
		ImageData:  imageData,
		ImageExt:   imageExt,
		BaseDomain: os.Getenv("PAGES_BASE_DOMAIN"),
		CFAPIToken: os.Getenv("CLOUDFLARE_API_TOKEN"),
		CFZoneID:   os.Getenv("CLOUDFLARE_ZONE_ID"),
	})
	if err != nil {
		log.Printf("error generating site: %v", err)
		http.Error(w, fmt.Sprintf("failed to generate site: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{RepoURL: repoURL})
}
