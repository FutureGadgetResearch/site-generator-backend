# site-generator-backend

A GCP Cloud Run Function (Go) that clones a Hugo template repo, injects custom data, and pushes it as a new site repo.

## Prerequisites

- Go 1.22+
- A [GitHub App](https://docs.github.com/en/apps/creating-github-apps) installed on your org with these permissions:
  - **Repository: Contents** — Read & Write
  - **Repository: Administration** — Read & Write (required for repo creation and deletion)
  - **Repository: Pages** — Read & Write
  - **Repository: Actions** — Read & Write (required for dispatching workflows)
- (For deployment) `gcloud` CLI
- (Optional) Cloudflare account for custom domain CNAME management

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `GITHUB_APP_ID` | Yes | — | GitHub App ID (numeric) |
| `GITHUB_APP_INSTALLATION_ID` | Yes | — | Installation ID for your org |
| `GITHUB_APP_PRIVATE_KEY` | Yes | — | PEM private key (full contents, not a file path) |
| `GITHUB_ORG` | No | `FutureGadgetResearch` | GitHub org for template and output repos |
| `PAGES_BASE_DOMAIN` | No | — | Base domain for custom GitHub Pages URLs (e.g. `35357670.xyz` → `site-name.35357670.xyz`) |
| `CLOUDFLARE_API_TOKEN` | No | — | Cloudflare API token for managing DNS records |
| `CLOUDFLARE_ZONE_ID` | No | — | Cloudflare Zone ID for the base domain |
| `PORT` | No | `8080` | Local server port |

## Running Locally

```bash
export GITHUB_APP_ID=123456
export GITHUB_APP_INSTALLATION_ID=78901234
export GITHUB_APP_PRIVATE_KEY="$(cat path/to/private-key.pem)"
```
```bash
go run cmd/server/main.go
```

The server starts on `http://localhost:8080`.

## Deploying to GCP (Cloud Build)

### 1. Create Secrets in Secret Manager

Store each secret in [Secret Manager](https://console.cloud.google.com/security/secret-manager):

| Secret Name | Value |
|---|---|
| `github-app-id` | GitHub App ID |
| `github-app-installation-id` | GitHub App Installation ID |
| `github-app-private-key` | PEM private key contents |
| `cloudflare-api-token` | Cloudflare API token |
| `cloudflare-zone-id` | Cloudflare Zone ID |

### 2. Grant Secret Manager Access

The Cloud Run service account needs the **Secret Manager Secret Accessor** role. Grant it at the project level:

```bash
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:PROJECT_NUMBER-compute@developer.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"
```

Or per-secret for least privilege:

```bash
gcloud secrets add-iam-policy-binding SECRET_NAME \
  --member="serviceAccount:PROJECT_NUMBER-compute@developer.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"
```

### 3. Cloud Build Trigger Configuration

Set up a Cloud Build trigger connected to this repository with:

| Setting | Value |
|---|---|
| Build type | Go |
| Build context directory | `/` |
| Entry point | `GenerateSite` |
| Function target | `GenerateSite` |

Configure environment variables on the Cloud Run function, referencing Secret Manager for sensitive values and setting plain text for non-sensitive ones:

**Secrets (reference from Secret Manager):**
- `GITHUB_APP_ID`
- `GITHUB_APP_INSTALLATION_ID`
- `GITHUB_APP_PRIVATE_KEY`
- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ZONE_ID`

**Plain text variables:**
- `GITHUB_ORG`
- `PAGES_BASE_DOMAIN`

### Deploying with GitHub Actions (alternative)

The `.github/workflows/deploy.yml` workflow deploys on push to `main` using Workload Identity Federation.

**GitHub Actions Secrets:**

| Secret | Description |
|---|---|
| `GH_APP_ID` | GitHub App ID |
| `GH_APP_INSTALLATION_ID` | GitHub App Installation ID |
| `GH_APP_PRIVATE_KEY` | PEM private key contents |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token |
| `CLOUDFLARE_ZONE_ID` | Cloudflare Zone ID |

**GitHub Actions Variables:**

| Variable | Description |
|---|---|
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | Workload Identity Provider resource name |
| `GCP_SERVICE_ACCOUNT` | GCP service account email |
| `GCP_REGION` | GCP region (e.g. `us-central1`) |
| `GCP_PROJECT_ID` | GCP project ID |
| `GITHUB_ORG` | GitHub org name |
| `PAGES_BASE_DOMAIN` | Base domain for Pages URLs |

## API Endpoints

A Postman collection with all endpoints pre-configured is available here:

[**Open in Postman**](https://cloudy-trinity-422143.postman.co/workspace/My-Workspace~388de6cc-3693-4f8d-8e54-64c532fc37a8/collection/10054838-1db26b95-aa02-4b37-bf24-73496bd708c9?action=share&creator=10054838&active-environment=10054838-3f2dc842-59a3-4887-ae77-a8807cebd64c)

### Generate Site

`POST /GenerateSite` — multipart/form-data

| Part | Type | Required | Description |
|---|---|---|---|
| `metadata` | Text | Yes | JSON string with `template`, `site_name`, `type`, and `data` fields |
| `image` | File | No | Primary image for the site (`.jpg`, `.jpeg`, `.png`, or `.webp`) |

Example `metadata` value:

```json
{
  "template": "wedding-template-1",
  "site_name": "jane-john-wedding",
  "type": "wedding",
  "data": {
    "couple": { "partner1": "Jane", "partner2": "John" },
    "event": { "date": "2026-06-15", "venue": "Central Park" }
  }
}
```

In Postman: set Body to **form-data**, add `metadata` as Text and `image` as File.

**Response** `200 OK`:

```json
{ "repo_url": "https://github.com/FutureGadgetResearch/jane-john-wedding" }
```

The `type` field determines the data filename inside the generated repo. For example, `"type": "wedding"` writes to `data/wedding.json`, while `"type": "scrapbook"` writes to `data/scrapbook.json`. The uploaded image is written to `static/images/primary.{ext}`.

### Check if a Repo Exists

`POST /RepoExists` — application/json

```json
{ "repo_name": "jane-john-wedding" }
```

**Response** `200 OK`:

```json
{ "exists": true }
```

### Delete a Site Repo

`POST /DeleteSite` — application/json

```json
{ "site_name": "jane-john-wedding" }
```

**Response**: `204 No Content` on success. Also deletes the Cloudflare CNAME record if Cloudflare is configured.
