//go:build !linux && !darwin

package platform

// NewServiceManager returns nil on unsupported platforms.
func NewServiceManager() ServiceManager {
	return nil
}
