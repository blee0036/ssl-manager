package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// Sentinel errors for DNS sync error classification
var (
	ErrSyncInProgress    = errors.New("sync already in progress")
	ErrDNSConfigNotFound = errors.New("thirdpart DNS config not found")
	ErrDNSConfigDisabled = errors.New("thirdpart DNS config disabled")
)

// DNSRecord represents a DNS record returned from a DNS provider API.
type DNSRecord struct {
	ID    string `json:"id"`
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
	dnsRepo        *repository.ThirdpartDNSRepository
	domainRepo     *repository.DomainRepository
	cfClient       CloudflareClient
	alerter        AlertSender
	runtimeCfg     *config.RuntimeConfig
	syncingConfigs sync.Map // configID -> bool
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

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	config := &model.ThirdpartDNS{
		Name:        input.Name,
		Type:        input.Type,
		APIToken:    input.APIToken,
		ConfigJSON:  input.ConfigJSON,
		MainDomains: mainDomains,
		Enabled:     enabled,
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
	// Concurrent sync protection: only one sync per configID at a time
	if _, loaded := s.syncingConfigs.LoadOrStore(configID, true); loaded {
		return nil, ErrSyncInProgress
	}
	defer s.syncingConfigs.Delete(configID)

	config, err := s.dnsRepo.GetByID(ctx, configID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDNSConfigNotFound
		}
		return nil, fmt.Errorf("failed to get thirdpart_dns config: %w", err)
	}

	if !config.Enabled {
		return nil, ErrDNSConfigDisabled
	}

	if s.cfClient == nil {
		return nil, fmt.Errorf("cloudflare client is not configured")
	}

	// Fetch DNS records from Cloudflare with completeness metadata
	fetchResult, err := s.fetchRecords(ctx, config)
	if err != nil {
		s.handleSyncFailure(ctx, config, nil, err.Error())
		return nil, fmt.Errorf("failed to fetch DNS records: %w", err)
	}

	// Safety check: if fetch is incomplete, do NOT make any local changes
	if !fetchResult.Complete {
		errMsg := fmt.Sprintf("incomplete fetch (visible_zones=%d, matched_zones=%d, records=%d): skipping all local changes",
			fetchResult.VisibleZoneCount, fetchResult.MatchedZoneCount, len(fetchResult.Records))
		s.handleSyncFailure(ctx, config, nil, errMsg)
		return nil, fmt.Errorf("incomplete DNS fetch: %s", errMsg)
	}

	// Sync records to local domain list (Create/Update/Delete)
	result, err := s.syncToLocalDomains(ctx, config, fetchResult)
	if err != nil {
		// Pass partial result (may contain already-committed changes) for traceability
		s.handleSyncFailure(ctx, config, result, err.Error())
		return nil, fmt.Errorf("failed to sync records to local domains: %w", err)
	}

	// Record sync success and auto-resolve previous alerts
	s.handleSyncSuccess(ctx, config, result)

	return result, nil
}

// GetSyncLogs retrieves sync logs for a configuration with pagination.
func (s *ThirdpartDNSService) GetSyncLogs(ctx context.Context, configID string, page, perPage int) ([]*model.ThirdpartDNSSyncLog, int, error) {
	return s.dnsRepo.GetSyncLogs(ctx, configID, page, perPage)
}

// ScanZones lists all Cloudflare zones accessible with the given token.
// 委托 cfClient.ListZones()，在 service 层统一 nil 检查。
func (s *ThirdpartDNSService) ScanZones(ctx context.Context, token string) ([]Zone, error) {
	if s.cfClient == nil {
		return nil, fmt.Errorf("cloudflare client not configured")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("api token cannot be empty")
	}
	return s.cfClient.ListZones(ctx, token)
}

// FetchResult contains fetched DNS records and completeness metadata for safety checks.
type FetchResult struct {
	Records          []DNSRecord
	VisibleZoneCount int  // Total zones returned by Cloudflare
	MatchedZoneCount int  // Zones matching main_domains
	Complete         bool // All zone/record requests succeeded and all records have valid IDs
}

// fetchRecords fetches DNS records from Cloudflare and returns completeness metadata.
// If any zone/record request fails or any record has an empty ID, Complete is set to false.
func (s *ThirdpartDNSService) fetchRecords(ctx context.Context, config *model.ThirdpartDNS) (*FetchResult, error) {
	result := &FetchResult{Complete: true}

	// List all zones
	zones, err := s.cfClient.ListZones(ctx, config.APIToken)
	if err != nil {
		return nil, fmt.Errorf("failed to list zones: %w", err)
	}
	result.VisibleZoneCount = len(zones)

	// Safety check 1: zero visible zones → sync failure
	if result.VisibleZoneCount == 0 {
		result.Complete = false
		return result, fmt.Errorf("zero visible zones returned, possible token permission issue")
	}

	// Filter zones by main_domains
	matchedZones := filterZonesByMainDomains(zones, config.MainDomains)
	result.MatchedZoneCount = len(matchedZones)

	// Safety check 2: main_domains configured but zero matches → sync failure
	if len(config.MainDomains) > 0 && result.MatchedZoneCount == 0 {
		result.Complete = false
		return result, fmt.Errorf("main_domains configured but no matching zones found")
	}

	// Fetch records for each matched zone
	recordTypes := []string{"A", "AAAA", "CNAME"}
	for _, zone := range matchedZones {
		records, err := s.cfClient.ListDNSRecords(ctx, config.APIToken, zone.ID, recordTypes)
		if err != nil {
			// Any zone/record request failure → mark incomplete
			log.Printf("[DNSSync] Failed to fetch records for zone %s: %v", zone.Name, err)
			result.Complete = false
			continue
		}
		// Safety check 3: verify every record has a non-empty ID
		for _, r := range records {
			if strings.TrimSpace(r.ID) == "" {
				log.Printf("[DNSSync] Record with empty ID in zone %s: name=%s type=%s", zone.Name, r.Name, r.Type)
				result.Complete = false
				break
			}
		}
		if !result.Complete {
			break
		}
		// Filter records by main_domains scope if configured
		if len(config.MainDomains) > 0 {
			var scoped []DNSRecord
			for _, r := range records {
				if isInMainDomainScope(normalizeHostname(r.Name), config.MainDomains) {
					scoped = append(scoped, r)
				}
			}
			result.Records = append(result.Records, scoped...)
		} else {
			result.Records = append(result.Records, records...)
		}
	}

	return result, nil
}

// normalizeHostname normalizes a hostname: lowercase + trim trailing dot.
func normalizeHostname(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

// isInMainDomainScope checks if a normalized hostname is within the main_domains scope.
// - If main_domains is empty, all hostnames are in scope (full sync mode).
// - Otherwise, hostname must equal a main_domain or be a subdomain of one.
// - All values are expected to be normalized before comparison.
func isInMainDomainScope(normalizedName string, mainDomains []string) bool {
	if len(mainDomains) == 0 {
		return true
	}
	for _, md := range mainDomains {
		normalizedMD := normalizeHostname(md)
		if normalizedName == normalizedMD {
			return true
		}
		if strings.HasSuffix(normalizedName, "."+normalizedMD) {
			return true
		}
	}
	return false
}

// filterZonesByMainDomains filters Cloudflare zones based on main_domains configuration.
// A zone is selected if any main_domain is the zone itself or a subdomain of it.
// If main_domains is empty, all zones are returned (full sync mode).
func filterZonesByMainDomains(zones []Zone, mainDomains []string) []Zone {
	if len(mainDomains) == 0 {
		return zones
	}

	// Normalize main_domains
	normalizedMDs := make([]string, len(mainDomains))
	for i, md := range mainDomains {
		normalizedMDs[i] = normalizeHostname(md)
	}

	var filtered []Zone
	for _, zone := range zones {
		normalizedZone := normalizeHostname(zone.Name)
		for _, nmd := range normalizedMDs {
			// zone covers main_domain: main_domain is the zone itself or a subdomain of zone
			if nmd == normalizedZone || strings.HasSuffix(nmd, "."+normalizedZone) {
				filtered = append(filtered, zone)
				break
			}
		}
	}
	return filtered
}

// syncToLocalDomains syncs fetched DNS records to the local domains table.
// It performs Create/Update/Delete based on dns_record_id as the identity key.
// Precondition: fetchResult.Complete == true (caller guarantees this).
func (s *ThirdpartDNSService) syncToLocalDomains(ctx context.Context, config *model.ThirdpartDNS, fetchResult *FetchResult) (*model.DNSSyncResult, error) {
	// Initialize all slices as empty arrays to ensure JSON outputs [] not null
	result := &model.DNSSyncResult{
		RecordsCount:   len(fetchResult.Records),
		NewDomains:     []string{},
		UpdatedDomains: []string{},
		RemovedDomains: []string{},
	}

	// Get existing domains for this config (source=cloudflare, thirdpart_dns_id=config.ID)
	existingDomains, err := s.domainRepo.List(ctx, model.DomainFilter{
		Source:         "cloudflare",
		ThirdpartDNSID: config.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list existing domains: %w", err)
	}

	// incomingRecordIDs is the set of every record.ID present in this sync's
	// fetch result, computed up front so the fallback candidate pool below can
	// correctly exclude existing rows that WILL be matched exactly by
	// existingByRecordID — otherwise a row already claimed by its own
	// dns_record_id could ALSO be claimed a second time via the hostname+type
	// fallback by an unrelated incoming record, corrupting its DNS fields.
	incomingRecordIDs := make(map[string]bool, len(fetchResult.Records))
	for _, record := range fetchResult.Records {
		incomingRecordIDs[record.ID] = true
	}

	// Build existingMap: prefer dns_record_id, fallback for legacy/re-created
	// records without a matching record ID. Unlike existingByRecordID (a strict
	// 1:1 map keyed by the stable Cloudflare record ID), existingByFallback is
	// keyed only by normalizeHostname+type and can legitimately hold MULTIPLE
	// domains per key (e.g. two A records, or an A and an AAAA record, for the
	// same hostname) — hostnames are NOT required to be globally unique, only
	// dns_record_id identity is. A row is added to the fallback pool only when
	// it will NOT be matched exactly via existingByRecordID this sync (its
	// DNSRecordID is empty — legacy data — or no longer present in this sync's
	// results — the Cloudflare-side record was deleted and recreated with a new
	// ID). This lets such a row be matched and UPDATED by hostname+type instead
	// of being deleted and re-created from scratch, which would silently reset
	// any user-modified fields (monitor_enabled, alert_ignored, linked_*) back
	// to their defaults.
	existingByRecordID := make(map[string]*model.Domain)   // dns_record_id → domain (dns_record_id != "")
	existingByFallback := make(map[string][]*model.Domain) // normalizeHostname+type → domains (possibly >1)
	for _, d := range existingDomains {
		if d.DNSRecordID != "" {
			existingByRecordID[d.DNSRecordID] = d
			if incomingRecordIDs[d.DNSRecordID] {
				// Will be matched exactly this sync; not a fallback candidate.
				continue
			}
		}
		fk := normalizeHostname(d.Name) + "\x00" + strings.ToUpper(d.DNSRecordType)
		existingByFallback[fk] = append(existingByFallback[fk], d)
	}

	// Create / Update phase
	syncedRecordIDs := make(map[string]bool)
	for _, record := range fetchResult.Records {
		syncedRecordIDs[record.ID] = true

		if existing, ok := existingByRecordID[record.ID]; ok {
			// Update: only update DNS-related fields, preserve user-modified fields
			if existing.Name != record.Name || existing.DNSRecordType != record.Type || existing.DNSRecordValue != record.Value {
				updates := map[string]interface{}{
					"name":             record.Name,
					"dns_record_type":  record.Type,
					"dns_record_value": record.Value,
				}
				if err := s.domainRepo.Update(ctx, existing.ID, updates); err != nil {
					return result, fmt.Errorf("failed to update domain %s: %w", existing.Name, err)
				}
				result.UpdatedDomains = append(result.UpdatedDomains, record.Name)
			}
			continue
		}

		// No exact dns_record_id match: look for a same-hostname+type existing
		// domain that has NOT already been claimed by another record in this
		// sync pass (this covers both legacy data with no dns_record_id at all,
		// and a record whose Cloudflare-side ID changed — deleted and recreated
		// with the same hostname/type). Claiming (removing from the fallback
		// slice) prevents two incoming records from both being matched to the
		// same pre-existing row when a hostname legitimately has multiple
		// records of the same type.
		fk := normalizeHostname(record.Name) + "\x00" + strings.ToUpper(record.Type)
		if candidates := existingByFallback[fk]; len(candidates) > 0 {
			oldDomain := candidates[0]
			existingByFallback[fk] = candidates[1:] // claim it

			updates := map[string]interface{}{
				"dns_record_id":    record.ID,
				"dns_record_type":  record.Type,
				"dns_record_value": record.Value,
			}
			if err := s.domainRepo.Update(ctx, oldDomain.ID, updates); err != nil {
				return result, fmt.Errorf("failed to migrate domain %s: %w", oldDomain.Name, err)
			}
			result.UpdatedDomains = append(result.UpdatedDomains, record.Name)
			continue
		}

		// Create: genuinely new record — directly construct *model.Domain and
		// call domainRepo.Create(). Does NOT use CreateDomainInput (only for
		// manual HTTP creation), does NOT go through DomainMonitorService.Create()
		// (which would trigger auto-probe).
		newDomain := &model.Domain{
			Name:           record.Name,
			Source:         "cloudflare",
			ThirdpartDNSID: config.ID,
			DNSRecordID:    record.ID,
			DNSRecordType:  record.Type,
			DNSRecordValue: record.Value,
			MonitorPort:    s.defaultPort(config),
			MonitorEnabled: true,
			AlertIgnored:   false,
		}
		if err := s.domainRepo.Create(ctx, newDomain); err != nil {
			return result, fmt.Errorf("failed to create domain %s: %w", record.Name, err)
		}
		result.NewDomains = append(result.NewDomains, record.Name)
	}

	// Build the set of existing domain IDs that were migrated/updated via the
	// fallback (hostname+type) match above, so the delete phase below does not
	// treat them as vanished — a row can be updated by ID one moment and still
	// need protecting from deletion in the same pass.
	claimedByFallback := make(map[string]bool)
	for _, existing := range existingDomains {
		fk := normalizeHostname(existing.Name) + "\x00" + strings.ToUpper(existing.DNSRecordType)
		stillUnclaimed := false
		for _, d := range existingByFallback[fk] {
			if d.ID == existing.ID {
				stillUnclaimed = true
				break
			}
		}
		if !stillUnclaimed {
			claimedByFallback[existing.ID] = true
		}
	}

	// Delete phase: only delete records meeting ALL conditions:
	// 1. source == "cloudflare"
	// 2. thirdpart_dns_id == current config ID
	// 3. within current main_domains scope
	// 4. dns_record_id not in this sync's results, AND not claimed via the
	//    hostname+type fallback match above (a record whose Cloudflare-side ID
	//    changed is claimed/updated, not deleted)
	for _, existing := range existingDomains {
		if existing.Source != "cloudflare" || existing.ThirdpartDNSID != config.ID {
			continue
		}
		normalized := normalizeHostname(existing.Name)
		if !isInMainDomainScope(normalized, config.MainDomains) {
			continue // Not in current scope, don't delete
		}
		if existing.DNSRecordID != "" && syncedRecordIDs[existing.DNSRecordID] {
			continue // Still exists under its current dns_record_id, don't delete
		}
		if claimedByFallback[existing.ID] {
			continue // Matched and updated via hostname+type fallback, don't delete
		}
		// Delete domain and its monitor results (domainRepo.Delete cascades)
		if err := s.domainRepo.Delete(ctx, existing.ID); err != nil {
			return result, fmt.Errorf("failed to delete vanished domain %s: %w", existing.Name, err)
		}
		result.RemovedDomains = append(result.RemovedDomains, existing.Name)
	}

	return result, nil
}

// defaultPort returns the configured default monitoring port for new sync domains.
func (s *ThirdpartDNSService) defaultPort(config *model.ThirdpartDNS) int {
	if s.runtimeCfg != nil {
		port := s.runtimeCfg.Get().DomainMonitor.DefaultPort
		if port > 0 {
			return port
		}
	}
	return 443
}

// saveSyncLog saves a sync log entry with full sync detail.
// If result is nil (e.g., sync failed before producing results), uses empty defaults.
func (s *ThirdpartDNSService) saveSyncLog(ctx context.Context, configID string, result *model.DNSSyncResult, status, errMsg string) {
	// Handle nil result gracefully to prevent panic on early failures
	safeResult := result
	if safeResult == nil {
		safeResult = &model.DNSSyncResult{
			NewDomains:     []string{},
			UpdatedDomains: []string{},
			RemovedDomains: []string{},
		}
	}

	newJSON, _ := json.Marshal(safeResult.NewDomains)
	updatedJSON, _ := json.Marshal(safeResult.UpdatedDomains)
	removedJSON, _ := json.Marshal(safeResult.RemovedDomains)

	syncLog := &model.ThirdpartDNSSyncLog{
		ThirdpartDNSID: configID,
		RecordsCount:   safeResult.RecordsCount,
		Status:         status,
		ErrorMessage:   errMsg,
		NewDomains:     string(newJSON),
		UpdatedDomains: string(updatedJSON),
		RemovedDomains: string(removedJSON),
		SyncedAt:       time.Now().UTC(),
	}
	// Best effort - don't fail the sync if log saving fails
	_ = s.dnsRepo.SaveSyncLog(ctx, syncLog)
}

// handleSyncFailure writes a failed sync log and sends a dns_sync_failed alert.
func (s *ThirdpartDNSService) handleSyncFailure(ctx context.Context, config *model.ThirdpartDNS, result *model.DNSSyncResult, errMsg string) {
	s.saveSyncLog(ctx, config.ID, result, "failed", errMsg)

	if s.alerter != nil {
		alertContent := fmt.Sprintf("DNS sync failed for config %s (%s): %s", config.Name, config.ID, errMsg)
		_ = s.alerter.SendAlert(ctx, "warning", "dns_sync_failed", "DNS Sync Failed", alertContent, "thirdpart_dns", config.ID)
	}
}

// handleSyncSuccess writes a success sync log and auto-resolves any previous dns_sync_failed alert.
func (s *ThirdpartDNSService) handleSyncSuccess(ctx context.Context, config *model.ThirdpartDNS, result *model.DNSSyncResult) {
	s.saveSyncLog(ctx, config.ID, result, "success", "")

	if s.alerter != nil {
		s.alerter.AutoResolve(ctx, "thirdpart_dns", config.ID, "dns_sync_failed")
	}
}
