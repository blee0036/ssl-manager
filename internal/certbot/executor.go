package certbot

import (
	"context"
	"fmt"
	"os/exec"
)

// DefaultExecutor implements CommandExecutor using os/exec.
type DefaultExecutor struct{}

// NewDefaultExecutor creates a new DefaultExecutor.
func NewDefaultExecutor() *DefaultExecutor {
	return &DefaultExecutor{}
}

// Execute runs a command and returns its combined output.
func (e *DefaultExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("command %q failed: %w", name, err)
	}
	return output, nil
}
