package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// genExternalURL generates a valid ExternalURL starting with http:// or https://
func genExternalURL() gopter.Gen {
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

// genListenAddr generates a valid listen address like ":8080" or "0.0.0.0:9090"
func genListenAddr() gopter.Gen {
	return gen.OneGenOf(
		gen.IntRange(1, 65535).Map(func(port int) string {
			return fmt.Sprintf(":%d", port)
		}),
		gen.IntRange(1, 65535).Map(func(port int) string {
			return fmt.Sprintf("0.0.0.0:%d", port)
		}),
	)
}

// genBinaryPath generates a non-empty binary path string
func genBinaryPath() gopter.Gen {
	return gen.AlphaString().SuchThat(func(s string) bool {
		return s != ""
	}).Map(func(s string) string {
		return "/usr/bin/" + s
	})
}

// genReadonlyConfig generates a valid ReadonlyConfig (if Enabled, ViewPassword must be non-empty)
func genReadonlyConfig() gopter.Gen {
	return gen.OneGenOf(
		// Disabled case: password can be anything
		gen.AlphaString().Map(func(pw string) ReadonlyConfig {
			return ReadonlyConfig{
				Enabled:      false,
				ViewPassword: pw,
			}
		}),
		// Enabled case: password must be non-empty
		gen.AlphaString().SuchThat(func(s string) bool {
			return s != ""
		}).Map(func(pw string) ReadonlyConfig {
			return ReadonlyConfig{
				Enabled:      true,
				ViewPassword: pw,
			}
		}),
	)
}

// genConfig generates a valid Config object with all fields within valid ranges
func genConfig() gopter.Gen {
	return gopter.CombineGens(
		genExternalURL(),
		genListenAddr(),
		gen.IntRange(1, 3600),  // HeartbeatTimeoutSeconds
		gen.IntRange(1, 3600),  // PollIntervalSeconds
		gen.IntRange(1, 365),   // DefaultBeforeDays
		genBinaryPath(),
		gen.AlphaString(),      // DataDir
		gen.AlphaString(),      // Email
		genReadonlyConfig(),
		gen.IntRange(1, 65535), // DefaultPort
		gen.IntRange(1, 1440),  // IntervalMinutes
	).Map(func(values []interface{}) Config {
		return Config{
			Server: ServerConfig{
				ExternalURL: values[0].(string),
				ListenAddr:  values[1].(string),
			},
			Agent: AgentConfig{
				HeartbeatTimeoutSeconds: values[2].(int),
				PollIntervalSeconds:     values[3].(int),
			},
			Alert: AlertConfig{
				DefaultBeforeDays: values[4].(int),
			},
			Certbot: CertbotConfig{
				BinaryPath: values[5].(string),
				DataDir:    values[6].(string),
				Email:      values[7].(string),
			},
			Readonly:      values[8].(ReadonlyConfig),
			DomainMonitor: DomainMonitorConfig{
				DefaultPort:     values[9].(int),
				IntervalMinutes: values[10].(int),
			},
		}
	})
}

// TestPropertyConfigSerializationRoundTrip verifies that for any valid Config object,
// serializing to JSON and deserializing back produces an equivalent Config.
//
// **Validates: Requirements 1.5**
func TestPropertyConfigSerializationRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 200
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("Config JSON serialization round-trip produces equivalent Config", prop.ForAll(
		func(original Config) bool {
			// Serialize to JSON
			data, err := json.Marshal(original)
			if err != nil {
				t.Logf("Marshal error: %v", err)
				return false
			}

			// Deserialize back
			var restored Config
			err = json.Unmarshal(data, &restored)
			if err != nil {
				t.Logf("Unmarshal error: %v", err)
				return false
			}

			// Assert equality
			return reflect.DeepEqual(original, restored)
		},
		genConfig(),
	))

	properties.TestingRun(t)
}
