package service

import "time"

// CertificateNeedsRenewal determines whether a certificate should be renewed.
// A certificate needs renewal when ALL of the following conditions are met:
// 1. auto_renew is true
// 2. days until expiry <= defaultBeforeDays
//
// If auto_renew is false, the certificate never needs renewal regardless of expiry.
// This function is used by the Scheduler to identify certificates that need renewal.
func CertificateNeedsRenewal(autoRenew bool, expireAt time.Time, now time.Time, defaultBeforeDays int) bool {
	if !autoRenew {
		return false
	}
	daysUntilExpiry := int(expireAt.Sub(now).Hours() / 24)
	return daysUntilExpiry <= defaultBeforeDays
}
