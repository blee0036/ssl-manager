package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// CertificateService handles certificate business logic.
type CertificateService struct {
	certRepo *repository.CertificateRepository
	db       *sql.DB
}

// NewCertificateService creates a new CertificateService.
func NewCertificateService(certRepo *repository.CertificateRepository, db *sql.DB) *CertificateService {
	return &CertificateService{
		certRepo: certRepo,
		db:       db,
	}
}

// ParsePEM parses a certificate PEM and extracts metadata.
// It extracts domains (CN + SANs), expiration time, issuer, SHA256 fingerprint,
// and validates the certificate chain if multiple certificates are present.
func (s *CertificateService) ParsePEM(certPEM []byte) (*model.CertMetadata, error) {
	certs, err := parseCertificateChain(certPEM)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates found in PEM data")
	}

	// The first certificate is the leaf (server) certificate
	leaf := certs[0]

	// Extract domains: combine Subject CN and SANs
	domains := extractDomains(leaf)

	// Calculate SHA256 fingerprint of the DER-encoded certificate
	fingerprint := sha256Fingerprint(leaf.Raw)

	// Validate chain
	chainValid := validateChain(certs)

	return &model.CertMetadata{
		Domains:           domains,
		ExpireAt:          leaf.NotAfter,
		Issuer:            leaf.Issuer.CommonName,
		FingerprintSHA256: fingerprint,
		ChainValid:        chainValid,
	}, nil
}

// ValidateKeyPair validates that a certificate and private key match.
// Supports RSA and ECDSA key types.
func (s *CertificateService) ValidateKeyPair(certPEM, keyPEM []byte) error {
	certs, err := parseCertificateChain(certPEM)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}
	if len(certs) == 0 {
		return fmt.Errorf("no certificates found in PEM data")
	}

	leaf := certs[0]
	privKey, err := parsePrivateKey(keyPEM)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Compare public keys
	switch pub := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		priv, ok := privKey.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("certificate has RSA public key but private key is not RSA")
		}
		if pub.N.Cmp(priv.N) != 0 || pub.E != priv.E {
			return fmt.Errorf("certificate and private key do not match")
		}
	case *ecdsa.PublicKey:
		priv, ok := privKey.(*ecdsa.PrivateKey)
		if !ok {
			return fmt.Errorf("certificate has ECDSA public key but private key is not ECDSA")
		}
		if pub.X.Cmp(priv.PublicKey.X) != 0 || pub.Y.Cmp(priv.PublicKey.Y) != 0 {
			return fmt.Errorf("certificate and private key do not match")
		}
	default:
		return fmt.Errorf("unsupported public key type: %T", leaf.PublicKey)
	}

	return nil
}

// Create uploads a new certificate (validates, saves files, saves metadata).
func (s *CertificateService) Create(ctx context.Context, input model.CreateCertInput) (*model.Certificate, error) {
	// Validate key pair
	if err := s.ValidateKeyPair(input.CertPEM, input.KeyPEM); err != nil {
		return nil, fmt.Errorf("key pair validation failed: %w", err)
	}

	// Parse certificate metadata using the full chain for validation
	var fullchainPEM []byte
	if len(input.ChainPEM) > 0 {
		fullchainPEM = append([]byte{}, input.CertPEM...)
		fullchainPEM = append(fullchainPEM, input.ChainPEM...)
	} else {
		fullchainPEM = input.CertPEM
	}

	meta, err := s.ParsePEM(fullchainPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate PEM: %w", err)
	}

	// If no chain is provided, mark chain_valid as false
	if len(input.ChainPEM) == 0 {
		meta.ChainValid = false
	}

	// Determine source: use input.Source if provided, otherwise default to "upload"
	source := "upload"
	if input.Source != "" {
		source = input.Source
	}

	// Create certificate record
	cert := &model.Certificate{
		Name:              input.Name,
		Domains:           meta.Domains,
		Source:            source,
		ExpireAt:          meta.ExpireAt,
		AutoRenew:         input.AutoRenew,
		Issuer:            meta.Issuer,
		FingerprintSHA256: meta.FingerprintSHA256,
		ChainValid:        meta.ChainValid,
		ThirdpartDNSID:    input.ThirdpartDNSID,
		RenewStatus:       "",
	}

	// Save metadata to database (this also creates the directory)
	if err := s.certRepo.Create(ctx, cert); err != nil {
		return nil, fmt.Errorf("failed to create certificate record: %w", err)
	}

	// Save certificate files
	if err := s.certRepo.SaveCertFiles(cert.ID, input.CertPEM, input.ChainPEM, fullchainPEM, input.KeyPEM); err != nil {
		return nil, fmt.Errorf("failed to save certificate files: %w", err)
	}

	return cert, nil
}

// Update updates an existing certificate's PEM content.
// Overwrites files, updates metadata, marks all associated MachineCertificates as pending sync.
func (s *CertificateService) Update(ctx context.Context, id string, input model.UpdateCertInput) (*model.Certificate, error) {
	// Verify certificate exists
	cert, err := s.certRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get certificate: %w", err)
	}

	updates := make(map[string]interface{})

	// If name is being updated
	if input.Name != nil {
		updates["name"] = *input.Name
	}

	// If auto_renew is being updated
	if input.AutoRenew != nil {
		updates["auto_renew"] = *input.AutoRenew
	}

	// If PEM content is being updated
	if len(input.CertPEM) > 0 && len(input.KeyPEM) > 0 {
		// Validate key pair
		if err := s.ValidateKeyPair(input.CertPEM, input.KeyPEM); err != nil {
			return nil, fmt.Errorf("key pair validation failed: %w", err)
		}

		// Parse new certificate metadata using full chain for validation
		var fullPEMForParse []byte
		if len(input.ChainPEM) > 0 {
			fullPEMForParse = append([]byte{}, input.CertPEM...)
			fullPEMForParse = append(fullPEMForParse, input.ChainPEM...)
		} else {
			fullPEMForParse = input.CertPEM
		}

		meta, err := s.ParsePEM(fullPEMForParse)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate PEM: %w", err)
		}

		// Build fullchain for file storage
		var fullchainPEM []byte
		if len(input.ChainPEM) > 0 {
			fullchainPEM = append([]byte{}, input.CertPEM...)
			fullchainPEM = append(fullchainPEM, input.ChainPEM...)
		} else {
			fullchainPEM = input.CertPEM
			meta.ChainValid = false
		}

		// Overwrite certificate files
		if err := s.certRepo.SaveCertFiles(id, input.CertPEM, input.ChainPEM, fullchainPEM, input.KeyPEM); err != nil {
			return nil, fmt.Errorf("failed to save certificate files: %w", err)
		}

		// If no chain was provided, remove any stale chain.pem file
		if len(input.ChainPEM) == 0 {
			os.Remove(filepath.Join(s.certRepo.CertDirPath(id), "chain.pem"))
		}

		// Update metadata fields
		updates["domains"] = meta.Domains
		updates["expire_at"] = meta.ExpireAt
		updates["issuer"] = meta.Issuer
		updates["fingerprint_sha256"] = meta.FingerprintSHA256
		updates["chain_valid"] = meta.ChainValid
	}

	// Apply updates to database
	if len(updates) > 0 {
		if err := s.certRepo.Update(ctx, id, updates); err != nil {
			return nil, fmt.Errorf("failed to update certificate: %w", err)
		}
	}

	// If PEM content was updated, mark associated MachineCertificates as pending sync
	if len(input.CertPEM) > 0 && len(input.KeyPEM) > 0 {
		if err := s.MarkAssociatedPendingSync(ctx, id); err != nil {
			return nil, fmt.Errorf("failed to mark associated machine certificates as pending sync: %w", err)
		}
	}

	// Return updated certificate
	updatedCert, err := s.certRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get updated certificate: %w", err)
	}

	_ = cert // suppress unused warning
	return updatedCert, nil
}

// Delete deletes a certificate and its files.
func (s *CertificateService) Delete(ctx context.Context, id string) error {
	return s.certRepo.Delete(ctx, id)
}

// GetByID retrieves a certificate by ID.
func (s *CertificateService) GetByID(ctx context.Context, id string) (*model.Certificate, error) {
	return s.certRepo.GetByID(ctx, id)
}

// List returns certificates with optional filtering.
func (s *CertificateService) List(ctx context.Context, filter model.CertFilter) ([]*model.Certificate, error) {
	return s.certRepo.List(ctx, filter)
}

// MarkAssociatedPendingSync marks all MachineCertificates for a certificate as pending sync
// and increments their config_revision.
func (s *CertificateService) MarkAssociatedPendingSync(ctx context.Context, certificateID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	query := `UPDATE machine_certificates SET last_deploy_status = 'pending', config_revision = config_revision + 1, updated_at = ? WHERE certificate_id = ?`
	_, err := s.db.ExecContext(ctx, query, now, certificateID)
	if err != nil {
		return fmt.Errorf("failed to mark associated machine certificates as pending sync: %w", err)
	}
	return nil
}

// UpdateSource updates arbitrary fields on a certificate record (used for setting source after certbot issuance).
func (s *CertificateService) UpdateSource(ctx context.Context, id string, updates map[string]interface{}) error {
	return s.certRepo.Update(ctx, id, updates)
}

// --- Helper functions ---

// parseCertificateChain parses all certificates from a PEM-encoded byte slice.
func parseCertificateChain(pemData []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := pemData

	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate: %w", err)
		}
		certs = append(certs, cert)
	}

	return certs, nil
}

// parsePrivateKey parses a PEM-encoded private key (RSA or ECDSA).
func parsePrivateKey(pemData []byte) (interface{}, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block for private key")
	}

	// Try PKCS8 first (works for both RSA and ECDSA)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		return key, nil
	}

	// Try RSA PKCS1
	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err == nil {
		return rsaKey, nil
	}

	// Try EC
	ecKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err == nil {
		return ecKey, nil
	}

	return nil, fmt.Errorf("failed to parse private key: unsupported key type")
}

// extractDomains extracts domains from a certificate (CN + SANs), deduplicated.
func extractDomains(cert *x509.Certificate) []string {
	domainSet := make(map[string]struct{})

	// Add Subject CN if it looks like a domain
	if cert.Subject.CommonName != "" {
		domainSet[cert.Subject.CommonName] = struct{}{}
	}

	// Add SANs
	for _, san := range cert.DNSNames {
		domainSet[san] = struct{}{}
	}

	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}

	return domains
}

// sha256Fingerprint calculates the SHA256 fingerprint of DER-encoded certificate data.
func sha256Fingerprint(der []byte) string {
	hash := sha256.Sum256(der)
	return hex.EncodeToString(hash[:])
}

// validateChain validates the certificate chain.
// Returns true if the chain can be verified (intermediate → root) or if it's a single self-signed cert.
// Returns false if chain validation fails.
func validateChain(certs []*x509.Certificate) bool {
	if len(certs) <= 1 {
		// Single certificate - check if it's self-signed
		if len(certs) == 1 {
			cert := certs[0]
			// A self-signed cert has the same issuer and subject
			if cert.Issuer.CommonName == cert.Subject.CommonName && cert.IsCA {
				return true
			}
			// Single non-self-signed cert without chain
			return false
		}
		return false
	}

	// Build intermediate pool from certificates after the leaf
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}

	// Verify the leaf certificate against the intermediates
	opts := x509.VerifyOptions{
		Intermediates: intermediates,
		// We don't require a system root pool for validation;
		// just check that the chain links together
		CurrentTime: time.Now(),
	}

	_, err := certs[0].Verify(opts)
	// If verification succeeds with system roots, chain is valid
	if err == nil {
		return true
	}

	// Try without system roots - just check chain linkage
	// If intermediates can sign the leaf, consider it valid enough
	for i := 0; i < len(certs)-1; i++ {
		if err := certs[i].CheckSignatureFrom(certs[i+1]); err != nil {
			return false
		}
	}

	return true
}
