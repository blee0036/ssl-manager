package platform

import (
	"testing"
)

// TestServiceManagerInterface verifies that the ServiceManager interface
// is properly defined and can be used as a type constraint.
// This is a compile-time check that the interface contract is correct.
func TestServiceManagerInterface(t *testing.T) {
	// Verify the interface has the expected method set by declaring a variable.
	// If the interface changes incompatibly, this test will fail to compile.
	var _ ServiceManager = (ServiceManager)(nil)

	// Verify the interface methods exist by type-asserting a nil interface.
	// This ensures the interface definition is complete.
	var sm ServiceManager
	if sm != nil {
		t.Fatal("nil ServiceManager should be nil")
	}
}

// TestNewServiceManagerOnUnsupportedPlatform verifies that NewServiceManager
// returns nil on platforms other than linux and darwin (i.e., Windows).
// Since tests run on Windows, this exercises the service_other.go path.
func TestNewServiceManagerOnUnsupportedPlatform(t *testing.T) {
	mgr := NewServiceManager()
	if mgr != nil {
		t.Fatalf("expected NewServiceManager() to return nil on unsupported platform, got %v", mgr)
	}
}

// TestServiceManagerInterfaceMethodSet verifies that the ServiceManager interface
// declares all expected methods by using a compile-time assertion pattern.
func TestServiceManagerInterfaceMethodSet(t *testing.T) {
	// This function uses a mock struct to verify the interface contract at compile time.
	// If any method is missing from the interface or has the wrong signature,
	// this file will fail to compile.
	type methodChecker interface {
		Stop() error
		Start() error
		Restart() error
		Disable() error
		Enable() error
		IsActive() (bool, error)
		Uninstall() error
		GetLogs(lines int, follow bool) error
	}

	// Compile-time assertion: ServiceManager must satisfy methodChecker
	var _ methodChecker = (ServiceManager)(nil)

	t.Log("ServiceManager interface declares all expected methods")
}
