//go:build darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

const (
	serviceLabel = "com.ssl-manager.agent"
	plistPath    = "/Library/LaunchDaemons/com.ssl-manager.agent.plist"
	logFilePath  = "/var/log/ssl-manager-agent.log"
	errLogPath   = "/var/log/ssl-manager-agent.err.log"
)

// launchdManager implements ServiceManager for macOS using launchd.
type launchdManager struct{}

// NewServiceManager 根据 runtime.GOOS 返回对应实现（macOS: launchd）
func NewServiceManager() ServiceManager {
	return &launchdManager{}
}

func (l *launchdManager) Restart() error {
	return exec.Command("launchctl", "kickstart", "-k", "system/"+serviceLabel).Run()
}

func (l *launchdManager) Stop() error {
	err := exec.Command("launchctl", "bootout", "system/"+serviceLabel).Run()
	if err != nil {
		// Fallback to legacy unload command
		return exec.Command("launchctl", "unload", plistPath).Run()
	}
	return nil
}

func (l *launchdManager) Start() error {
	err := exec.Command("launchctl", "bootstrap", "system", plistPath).Run()
	if err != nil {
		// Fallback to legacy load command
		return exec.Command("launchctl", "load", plistPath).Run()
	}
	return nil
}

func (l *launchdManager) Disable() error {
	// launchd doesn't have a separate disable concept for system daemons; same as Stop
	return l.Stop()
}

func (l *launchdManager) Enable() error {
	// launchd doesn't have a separate enable concept for system daemons; same as Start
	return l.Start()
}

func (l *launchdManager) IsActive() (bool, error) {
	err := exec.Command("launchctl", "list", serviceLabel).Run()
	if err == nil {
		return true, nil
	}
	// Exit code != 0 means service is not loaded/active
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}
	// Other errors (e.g., launchctl not found) are real errors
	return false, err
}

func (l *launchdManager) Uninstall() error {
	// Bootout the service (ignore error if already unloaded)
	_ = exec.Command("launchctl", "bootout", "system/"+serviceLabel).Run()

	// Delete plist file
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}

	// Delete log files
	if err := os.Remove(logFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove log file: %w", err)
	}
	if err := os.Remove(errLogPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove error log file: %w", err)
	}

	return nil
}

func (l *launchdManager) GetLogs(lines int, follow bool) error {
	if follow {
		cmd := exec.Command("tail", "-f", logFilePath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Read last N lines from log file
	return tailFile(logFilePath, lines)
}

// tailFile reads the last n lines from the given file and prints them to stdout.
func tailFile(path string, n int) error {
	cmd := exec.Command("tail", "-n", strconv.Itoa(n), path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}


