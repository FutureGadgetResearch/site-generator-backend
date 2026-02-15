package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type dnsRecord struct {
	ID string `json:"id"`
}

type listResponse struct {
	Result []dnsRecord `json:"result"`
}

type cnamPayload struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// EnsureCNAME creates or updates a CNAME record in Cloudflare DNS.
// name is the subdomain (e.g. "my-site"), target is the CNAME target
// (e.g. "org.github.io"), and zone is the Cloudflare zone (e.g. "example.com").
func EnsureCNAME(ctx context.Context, apiToken, zoneID, name, zone, target string) error {
	fqdn := name + "." + zone

	// Check if record already exists
	recordID, err := findRecord(ctx, apiToken, zoneID, fqdn)
	if err != nil {
		return fmt.Errorf("looking up existing CNAME: %w", err)
	}

	payload := cnamPayload{
		Type:    "CNAME",
		Name:    name,
		Content: target,
		TTL:     1,
		Proxied: false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling DNS payload: %w", err)
	}

	var method, url string
	if recordID != "" {
		method = http.MethodPut
		url = fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, recordID)
	} else {
		method = http.MethodPost
		url = fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", zoneID)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building DNS request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling Cloudflare API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Cloudflare API returned %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// DeleteCNAME removes a CNAME record from Cloudflare DNS if it exists.
func DeleteCNAME(ctx context.Context, apiToken, zoneID, name, zone string) error {
	fqdn := name + "." + zone

	recordID, err := findRecord(ctx, apiToken, zoneID, fqdn)
	if err != nil {
		return fmt.Errorf("looking up CNAME for deletion: %w", err)
	}
	if recordID == "" {
		return nil
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, recordID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("building delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling Cloudflare API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Cloudflare API returned %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

func findRecord(ctx context.Context, apiToken, zoneID, fqdn string) (string, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?type=CNAME&name=%s", zoneID, fqdn)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result listResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding Cloudflare response: %w", err)
	}

	if len(result.Result) > 0 {
		return result.Result[0].ID, nil
	}
	return "", nil
}
