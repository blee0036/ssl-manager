package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// genServerURL generates a valid server URL starting with http:// or https://
func genServerURL() gopter.Gen {
	return gen.OneGenOf(
		gen.AlphaString().Map(func(s string) string {
			if s == "" {
				s = "localhost"
			}
			return "http://" + s
		}),
		gen.AlphaString().Map(func(s string) string {
			if s == "" {
				s = "localhost"
			}
			return "https://" + s
		}),
	)
}

// genNonEmptyAlpha generates a non-empty alphanumeric string
func genNonEmptyAlpha() gopter.Gen {
	return gen.AlphaString().SuchThat(func(s string) bool {
		return s != ""
	})
}

// genLogLevel generates a valid log level string
func genLogLevel() gopter.Gen {
	return gen.OneConstOf("debug", "info", "warn", "error")
}

// genAgentConfig generates a valid AgentConfig with all required fields populated
func genAgentConfig() gopter.Gen {
	return gopter.CombineGens(
		genServerURL(),
		genNonEmptyAlpha(),
		genNonEmptyAlpha(),
		gen.IntRange(1, 3600),
		genLogLevel(),
	).Map(func(values []interface{}) *AgentConfig {
		return &AgentConfig{
			ServerURL:           values[0].(string),
			MachineID:           values[1].(string),
			AgentToken:          values[2].(string),
			PollIntervalSeconds: values[3].(int),
			LogLevel:            values[4].(string),
		}
	})
}

// TestPropertyAgentConfigYAMLRoundTrip verifies that for any valid AgentConfig,
// serializing to YAML (via SaveConfig) and deserializing back (via LoadConfig)
// produces an equivalent config.
//
// **Validates: Requirements 14.4**
func TestPropertyAgentConfigYAMLRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 200
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("AgentConfig YAML round-trip produces equivalent config", prop.ForAll(
		func(original *AgentConfig) bool {
			// Create a temp directory for the test
			tmpDir, err := os.MkdirTemp("", "agent-config-test-*")
			if err != nil {
				t.Logf("Failed to create temp dir: %v", err)
				return false
			}
			defer os.RemoveAll(tmpDir)

			configPath := filepath.Join(tmpDir, "config.yaml")

			// Save the config to a temp file
			if err := SaveConfig(configPath, original); err != nil {
				t.Logf("SaveConfig error: %v", err)
				return false
			}

			// Load it back
			restored, err := LoadConfig(configPath)
			if err != nil {
				t.Logf("LoadConfig error: %v", err)
				return false
			}

			// Verify all fields match
			return reflect.DeepEqual(original, restored)
		},
		genAgentConfig(),
	))

	properties.TestingRun(t)
}
