//go:build linux

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

const (
	serviceName = "ssl-manager-agent"
	unitFilePath = "/etc/systemd/system/ssl-manager-agent.service"
)

// systemdManager implements ServiceManager for Linux using systemd.
type systemdManager struct{}

// NewServiceManager 根据 runtime.GOOS 返回对应实现（Linux: systemd）
func NewServiceManager() ServiceManager {
	return &systemdManager{}
}

func (s *systemdManager) Stop() error {
	return exec.Command("systemctl", "stop", serviceName).Run()
}

func (s *systemdManager) Start() error {
	return exec.Command("systemctl", "start", serviceName).Run()
}

func (s *systemdManager) Restart() error {
	return exec.Command("systemctl", "restart", serviceName).Run()
}

func (s *systemdManager) Disable() error {
	return exec.Command("systemctl", "disable", serviceName).Run()
}

func (s *systemdManager) Enable() error {
	return exec.Command("systemctl", "enable", serviceName).Run()
}

func (s *systemdManager) IsActive() (bool, error) {
	err := exec.Command("systemctl", "is-active", serviceName).Run()
	if err == nil {
		return true, nil
	}
	// Exit code != 0 means service is not active; this is not an error condition
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}
	// Other errors (e.g., systemctl not found) are real errors
	return false, err
}

func (s *systemdManager) Uninstall() error {
	// Remove the unit file
	if err := os.Remove(unitFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}
	// Reload systemd daemon to pick up the removal
	return exec.Command("systemctl", "daemon-reload").Run()
}

func (s *systemdManager) GetLogs(lines int, follow bool) error {
	args := []string{"-u", serviceName, "--no-pager"}

	if follow {
		args = append(args, "-f")
	}

	args = append(args, "-n", strconv.Itoa(lines))

	cmd := exec.Command("journalctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
