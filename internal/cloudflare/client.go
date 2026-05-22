package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

const baseURL = "https://api.cloudflare.com/client/v4"

// Client implements service.CloudflareClient using the Cloudflare API v4.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new Cloudflare API client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// VerifyToken verifies that the given API token is valid by calling the Cloudflare token verify endpoint.
func (c *Client) VerifyToken(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/user/tokens/verify", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to verify token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token verification failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode verify response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("token is not valid")
	}

	return nil
}

// ListZones lists all DNS zones accessible with the given token.
func (c *Client) ListZones(ctx context.Context, token string) ([]service.Zone, error) {
	var allZones []service.Zone
	page := 1

	for {
		url := fmt.Sprintf("%s/zones?page=%d&per_page=50", baseURL, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to list zones: %w", err)
		}

		var result struct {
			Success bool `json:"success"`
			Result  []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"result"`
			ResultInfo struct {
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode zones response: %w", err)
		}
		resp.Body.Close()

		if !result.Success {
			return nil, fmt.Errorf("list zones request failed")
		}

		for _, z := range result.Result {
			allZones = append(allZones, service.Zone{
				ID:   z.ID,
				Name: z.Name,
			})
		}

		if page >= result.ResultInfo.TotalPages || len(result.Result) == 0 {
			break
		}
		page++
	}

	return allZones, nil
}

// ListDNSRecords lists DNS records for a zone, filtered by record types.
func (c *Client) ListDNSRecords(ctx context.Context, token string, zoneID string, types []string) ([]service.DNSRecord, error) {
	var allRecords []service.DNSRecord

	for _, recordType := range types {
		page := 1
		for {
			url := fmt.Sprintf("%s/zones/%s/dns_records?type=%s&page=%d&per_page=100", baseURL, zoneID, recordType, page)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := c.httpClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("failed to list DNS records: %w", err)
			}

			var result struct {
				Success bool `json:"success"`
				Result  []struct {
					Name    string `json:"name"`
					Type    string `json:"type"`
					Content string `json:"content"`
				} `json:"result"`
				ResultInfo struct {
					TotalPages int `json:"total_pages"`
				} `json:"result_info"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				return nil, fmt.Errorf("failed to decode DNS records response: %w", err)
			}
			resp.Body.Close()

			if !result.Success {
				return nil, fmt.Errorf("list DNS records request failed for zone %s", zoneID)
			}

			for _, r := range result.Result {
				allRecords = append(allRecords, service.DNSRecord{
					Name:  r.Name,
					Type:  r.Type,
					Value: r.Content,
				})
			}

			if page >= result.ResultInfo.TotalPages || len(result.Result) == 0 {
				break
			}
			page++
		}
	}

	return allRecords, nil
}

// Ensure Client implements CloudflareClient at compile time.
var _ service.CloudflareClient = (*Client)(nil)
