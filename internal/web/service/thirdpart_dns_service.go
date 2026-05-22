package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// DNSRecord represents a DNS record returned from a DNS provider API.
type DNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Zone represents a DNS zone returned from a DNS provider API.
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CloudflareClient is an interface for interacting with the Cloudflare API.
// It is designed to be mockable for testing.
type CloudflareClient interface {
	// VerifyToken verifies that the given API token is valid.
	VerifyToken(ctx context.Context, token string) error
	// ListZones lists all DNS zones accessible with the given token.
	ListZones(ctx context.Context, token string) ([]Zone, error)
	// ListDNSRecords lists DNS records for a zone, filtered by record types.
	ListDNSRecords(ctx context.Context, token string, zoneID string, types []string) ([]DNSRecord, error)
}

// ThirdpartDNSService handles third-party DNS upstream configuration and sync logic.
type ThirdpartDNSService struct {
	dnsRepo    *repository.ThirdpartDNSRepository
	domainRepo *repository.DomainRepository
	cfClient   CloudflareClient
	alerter    AlertSender
	runtimeCfg *config.RuntimeConfig
}

// NewThirdpartDNSService creates a new ThirdpartDNSService.
func NewThirdpartDNSService(
	dnsRepo *repository.ThirdpartDNSRepository,
	domainRepo *repository.DomainRepository,
	cfClient CloudflareClient,
	alerter AlertSender,
	runtimeCfg *config.RuntimeConfig,
) *ThirdpartDNSService {
	return &ThirdpartDNSService{
		dnsRepo:    dnsRepo,
		domainRepo: domainRepo,
		cfClient:   cfClient,
		alerter:    alerter,
		runtimeCfg: runtimeCfg,
	}
}

// SetCloudflareClient sets a custom Cloudflare client (for testing).
func (s *ThirdpartDNSService) SetCloudflareClient(client CloudflareClient) {
	s.cfClient = client
}

// CreateConfig creates a new third-party DNS upstream configuration.
// It validates the API token before saving.
func (s *ThirdpartDNSService) CreateConfig(ctx context.Context, input model.CreateThirdpartDNSInput) (*model.ThirdpartDNS, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if strings.TrimSpace(input.APIToken) == "" {
		return nil, fmt.Errorf("api_token cannot be empty")
	}
	if input.Type == "" {
		input.Type = "cloudflare"
	}
	if input.Type != "cloudflare" {
		return nil, fmt.Errorf("unsupported DNS type: %s (only 'cloudflare' is supported)", input.Type)
	}

	// Verify the API token is valid
	if s.cfClient != nil {
		if err := s.cfClient.VerifyToken(ctx, input.APIToken); err != nil {
			return nil, fmt.Errorf("API token verification failed: %w", err)
		}
	}

	mainDomains := input.MainDomains
	if mainDomains == nil {
		mainDomains = []string{}
	}

	config := &model.ThirdpartDNS{
		Name:        input.Name,
		Type:        input.Type,
		APIToken:    input.APIToken,
		ConfigJSON:  input.ConfigJSON,
		MainDomains: mainDomains,
		Enabled:     true,
	}

	if config.ConfigJSON == "" {
		config.ConfigJSON = "{}"
	}

	if err := s.dnsRepo.Create(ctx, config); err != nil {
		return nil, fmt.Errorf("failed to create thirdpart_dns config: %w", err)
	}

	return config, nil
}

// UpdateConfig updates an existing third-party DNS upstream configuration.
func (s *ThirdpartDNSService) UpdateConfig(ctx context.Context, id string, input model.UpdateThirdpartDNSInput) (*model.ThirdpartDNS, error) {
	// Verify config exists
	_, err := s.dnsRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get thirdpart_dns config: %w", err)
	}

	updates := make(map[string]interface{})

	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			return nil, fmt.Errorf("name cannot be empty")
		}
		updates["name"] = *input.Name
	}

	if input.APIToken != nil {
		if strings.TrimSpace(*input.APIToken) == "" {
			return nil, fmt.Errorf("api_token cannot be empty")
		}
		// Verify the new token
		if s.cfClient != nil {
			if err := s.cfClient.VerifyToken(ctx, *input.APIToken); err != nil {
				return nil, fmt.Errorf("API token verification failed: %w", err)
			}
		}
		updates["api_token"] = *input.APIToken
	}

	if input.ConfigJSON != nil {
		updates["config_json"] = *input.ConfigJSON
	}

	if input.MainDomains != nil {
		updates["main_domains"] = input.MainDomains
	}

	if input.Enabled != nil {
		updates["enabled"] = *input.Enabled
	}

	if err := s.dnsRepo.Update(ctx, id, updates); err != nil {
		return nil, fmt.Errorf("failed to update thirdpart_dns config: %w", err)
	}

	return s.dnsRepo.GetByID(ctx, id)
}

// DeleteConfig deletes a third-party DNS upstream configuration.
func (s *ThirdpartDNSService) DeleteConfig(ctx context.Context, id string) error {
	if err := s.dnsRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete thirdpart_dns config: %w", err)
	}
	return nil
}

// GetConfig retrieves a third-party DNS upstream configuration by ID.
func (s *ThirdpartDNSService) GetConfig(ctx context.Context, id string) (*model.ThirdpartDNS, error) {
	return s.dnsRepo.GetByID(ctx, id)
}

// ListConfigs returns all third-party DNS upstream configurations.
func (s *ThirdpartDNSService) ListConfigs(ctx context.Context) ([]*model.ThirdpartDNS, error) {
	return s.dnsRepo.List(ctx)
}

// SyncRecords synchronizes DNS records from the upstream provider.
// If main_domains is empty, it fetches all records from all zones.
// If main_domains is non-empty, it only fetches A/AAAA/CNAME records within those domains.
func (s *ThirdpartDNSService) SyncRecords(ctx context.Context, configID string) (*model.DNSSyncResult, error) {
	config, err := s.dnsRepo.GetByID(ctx, configID)
	if err != nil {
		return nil, fmt.Errorf("failed to get thirdpart_dns config: %w", err)
	}

	if !config.Enabled {
		return nil, fmt.Errorf("thirdpart_dns config %s is disabled", configID)
	}

	if s.cfClient == nil {
		return nil, fmt.Errorf("cloudflare client is not configured")
	}

	// Fetch DNS records from Cloudflare
	records, err := s.fetchRecords(ctx, config)
	if err != nil {
		// Record sync failure
		s.saveSyncLog(ctx, configID, 0, "failed", err.Error())

		// Send alert
		if s.alerter != nil {
			alertContent := fmt.Sprintf("DNS sync failed for config %s (%s): %v", config.Name, config.ID, err)
			_ = s.alerter.SendAlert(ctx, "warning", "dns_sync_failed", "DNS Sync Failed", alertContent, "thirdpart_dns", config.ID)
		}

		return nil, fmt.Errorf("failed to fetch DNS records: %w", err)
	}

	// Sync records to local domain list
	result, err := s.syncToLocalDomains(ctx, config, records)
	if err != nil {
		s.saveSyncLog(ctx, configID, 0, "failed", err.Error())
		return nil, fmt.Errorf("failed to sync records to local domains: %w", err)
	}

	// Record sync success
	s.saveSyncLog(ctx, configID, result.RecordsCount, "success", "")

	// Auto-resolve any previous dns_sync_failed alert for this config
	if s.alerter != nil {
		s.alerter.AutoResolve(ctx, "thirdpart_dns", config.ID, "dns_sync_failed")
	}

	return result, nil
}

// GetSyncLogs retrieves sync logs for a configuration.
func (s *ThirdpartDNSService) GetSyncLogs(ctx context.Context, configID string) ([]*model.ThirdpartDNSSyncLog, error) {
	return s.dnsRepo.GetSyncLogs(ctx, configID)
}

// fetchRecords fetches DNS records from Cloudflare based on the configuration.
func (s *ThirdpartDNSService) fetchRecords(ctx context.Context, config *model.ThirdpartDNS) ([]DNSRecord, error) {
	// List all zones
	zones, err := s.cfClient.ListZones(ctx, config.APIToken)
	if err != nil {
		return nil, fmt.Errorf("failed to list zones: %w", err)
	}

	// Filter zones by main_domains if specified
	filteredZones := s.filterZones(zones, config.MainDomains)

	// Record types to fetch
	recordTypes := []string{"A", "AAAA", "CNAME"}

	var allRecords []DNSRecord
	for _, zone := range filteredZones {
		records, err := s.cfClient.ListDNSRecords(ctx, config.APIToken, zone.ID, recordTypes)
		if err != nil {
			return nil, fmt.Errorf("failed to list DNS records for zone %s: %w", zone.Name, err)
		}

		// If main_domains is non-empty, filter records to only those within the specified domains
		if len(config.MainDomains) > 0 {
			records = s.filterRecordsByDomains(records, config.MainDomains)
		}

		allRecords = append(allRecords, records...)
	}

	return allRecords, nil
}

// filterZones filters zones based on main_domains configuration.
// If main_domains is empty, all zones are returned.
// If main_domains is non-empty, only zones matching the specified domains are returned.
func (s *ThirdpartDNSService) filterZones(zones []Zone, mainDomains []string) []Zone {
	if len(mainDomains) == 0 {
		return zones
	}

	var filtered []Zone
	for _, zone := range zones {
		for _, domain := range mainDomains {
			if zone.Name == domain || strings.HasSuffix(zone.Name, "."+domain) {
				filtered = append(filtered, zone)
				break
			}
		}
	}

	return filtered
}

// filterRecordsByDomains filters DNS records to only include those within the specified main domains.
func (s *ThirdpartDNSService) filterRecordsByDomains(records []DNSRecord, mainDomains []string) []DNSRecord {
	var filtered []DNSRecord
	for _, record := range records {
		for _, domain := range mainDomains {
			if record.Name == domain || strings.HasSuffix(record.Name, "."+domain) {
				filtered = append(filtered, record)
				break
			}
		}
	}
	return filtered
}

// syncToLocalDomains syncs fetched DNS records to the local domains table.
// It creates new Domain entries for records not already present and optionally creates Domain_Monitor objects.
func (s *ThirdpartDNSService) syncToLocalDomains(ctx context.Context, config *model.ThirdpartDNS, records []DNSRecord) (*model.DNSSyncResult, error) {
	// Get existing domains for this thirdpart_dns config
	existingDomains, err := s.domainRepo.List(ctx, model.DomainFilter{
		ThirdpartDNSID: config.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list existing domains: %w", err)
	}

	// Build a map of existing domain names for quick lookup
	existingMap := make(map[string]*model.Domain)
	for _, d := range existingDomains {
		existingMap[d.Name] = d
	}

	result := &model.DNSSyncResult{
		RecordsCount:   len(records),
		NewDomains:     []string{},
		UpdatedDomains: []string{},
	}

	for _, record := range records {
		existing, found := existingMap[record.Name]
		if !found {
			// Create new domain entry
			monitorPort := 443
			if s.runtimeCfg != nil {
				monitorPort = s.runtimeCfg.Get().DomainMonitor.DefaultPort
			}
			domain := &model.Domain{
				Name:           record.Name,
				Source:         "cloudflare",
				ThirdpartDNSID: config.ID,
				DNSRecordType:  record.Type,
				DNSRecordValue: record.Value,
				MonitorPort:    monitorPort,
				MonitorEnabled: true,
			}

			if err := s.domainRepo.Create(ctx, domain); err != nil {
				return nil, fmt.Errorf("failed to create domain %s: %w", record.Name, err)
			}

			result.NewDomains = append(result.NewDomains, record.Name)
		} else {
			// Update existing domain's DNS record info if changed
			if existing.DNSRecordType != record.Type || existing.DNSRecordValue != record.Value {
				updates := map[string]interface{}{
					"dns_record_type":  record.Type,
					"dns_record_value": record.Value,
				}
				if err := s.domainRepo.Update(ctx, existing.ID, updates); err != nil {
					return nil, fmt.Errorf("failed to update domain %s: %w", record.Name, err)
				}
				result.UpdatedDomains = append(result.UpdatedDomains, record.Name)
			}
		}
	}

	return result, nil
}

// saveSyncLog saves a sync log entry.
func (s *ThirdpartDNSService) saveSyncLog(ctx context.Context, configID string, recordsCount int, status, errMsg string) {
	log := &model.ThirdpartDNSSyncLog{
		ThirdpartDNSID: configID,
		RecordsCount:   recordsCount,
		Status:         status,
		ErrorMessage:   errMsg,
		SyncedAt:       time.Now().UTC(),
	}
	// Best effort - don't fail the sync if log saving fails
	_ = s.dnsRepo.SaveSyncLog(ctx, log)
}
