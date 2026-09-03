package certbot

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/config"
)

// Feature: ux-improvements-batch1, Property 1: effectiveDataDir Path Consistency
// **Validates: Requirements 1.1, 1.3, 1.6, 1.7**

func TestProperty_EffectiveDataDirConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 200
	properties := gopter.NewProperties(parameters)

	// Generator for non-empty DataDir path strings (e.g. "./custom/abcdef")
	nonEmptyPathGen := gen.RegexMatch(`\./[a-z]{1,10}/[a-z]{1,20}`)

	// Generator for domain-like strings
	domainNameGen := gen.RegexMatch(`[a-z]{2,10}\.[a-z]{2,5}`)

	// Generator for either non-empty path or empty string
	anyDataDirGen := gen.OneGenOf(nonEmptyPathGen, gen.Const(""))

	// Property 1: when DataDir is non-empty, effectiveDataDir returns the configured value
	properties.Property("non-empty DataDir returns configured value", prop.ForAll(
		func(dataDir string) bool {
			rCfg := config.NewRuntimeConfig(buildCfgWithDataDir(dataDir))
			w := NewCertbotWrapper(rCfg, &mockExecutor{})
			return w.effectiveDataDir() == dataDir
		},
		nonEmptyPathGen,
	))

	// Property 2: when DataDir is empty, effectiveDataDir returns Certbot's
	// native default config directory.
	properties.Property("empty DataDir returns default path", prop.ForAll(
		func(_ int) bool {
			rCfg := config.NewRuntimeConfig(buildCfgWithDataDir(""))
			w := NewCertbotWrapper(rCfg, &mockExecutor{})
			return w.effectiveDataDir() == "/etc/letsencrypt"
		},
		gen.Int(),
	))

	// Property 3: certOutputDir always uses effectiveDataDir as prefix (path consistency)
	properties.Property("certOutputDir uses effectiveDataDir as prefix", prop.ForAll(
		func(dataDir string, domain string) bool {
			rCfg := config.NewRuntimeConfig(buildCfgWithDataDir(dataDir))
			w := NewCertbotWrapper(rCfg, &mockExecutor{})

			base := w.effectiveDataDir()
			outputDir := w.certOutputDir(domain)

			// certOutputDir should be effectiveDataDir/live/<domain>
			expected := filepath.Join(base, "live", domain)
			return outputDir == expected
		},
		anyDataDirGen,
		domainNameGen,
	))

	// Property 4: buildCertbotArgs --config-dir value always equals effectiveDataDir return value
	properties.Property("buildCertbotArgs config-dir equals effectiveDataDir", prop.ForAll(
		func(dataDir string) bool {
			rCfg := config.NewRuntimeConfig(buildCfgWithDataDir(dataDir))
			w := NewCertbotWrapper(rCfg, &mockExecutor{})

			args := w.buildCertbotArgs([]string{"example.com"}, "test@example.com")
			base := w.effectiveDataDir()

			// Find --config-dir value in args
			configDirVal := findArgValue(args, "--config-dir")
			if configDirVal == "" {
				return false
			}

			return configDirVal == base
		},
		anyDataDirGen,
	))

	// Property 5: buildCertbotArgs --work-dir and --logs-dir use effectiveDataDir as prefix
	properties.Property("buildCertbotArgs work-dir and logs-dir use effectiveDataDir prefix", prop.ForAll(
		func(dataDir string) bool {
			rCfg := config.NewRuntimeConfig(buildCfgWithDataDir(dataDir))
			w := NewCertbotWrapper(rCfg, &mockExecutor{})

			args := w.buildCertbotArgs([]string{"example.com"}, "test@example.com")
			base := w.effectiveDataDir()

			workDirVal := findArgValue(args, "--work-dir")
			logsDirVal := findArgValue(args, "--logs-dir")

			if workDirVal == "" || logsDirVal == "" {
				return false
			}

			expectedWorkDir := filepath.Join(base, "work")
			expectedLogsDir := filepath.Join(base, "logs")

			return workDirVal == expectedWorkDir && logsDirVal == expectedLogsDir
		},
		anyDataDirGen,
	))

	// Property 6: all path functions use the same base for any given config
	// Note: filepath.Join may normalize paths differently on Windows vs Linux,
	// so we use filepath.Clean for consistent comparison.
	properties.Property("all path functions use consistent base", prop.ForAll(
		func(dataDir string, domain string) bool {
			rCfg := config.NewRuntimeConfig(buildCfgWithDataDir(dataDir))
			w := NewCertbotWrapper(rCfg, &mockExecutor{})

			base := w.effectiveDataDir()
			outputDir := w.certOutputDir(domain)
			args := w.buildCertbotArgs([]string{domain}, "test@example.com")

			configDirVal := findArgValue(args, "--config-dir")
			workDirVal := findArgValue(args, "--work-dir")
			logsDirVal := findArgValue(args, "--logs-dir")

			// configDir must exactly equal base (both are raw effectiveDataDir value)
			if configDirVal != base {
				return false
			}

			// For filepath.Join'd paths, normalize both sides before prefix check
			// because filepath.Join may strip "./" on Windows
			cleanBase := filepath.Clean(base)
			cleanOutput := filepath.Clean(outputDir)
			cleanWork := filepath.Clean(workDirVal)
			cleanLogs := filepath.Clean(logsDirVal)

			return strings.HasPrefix(cleanOutput, cleanBase) &&
				strings.HasPrefix(cleanWork, cleanBase) &&
				strings.HasPrefix(cleanLogs, cleanBase)
		},
		anyDataDirGen,
		domainNameGen,
	))

	properties.TestingRun(t)
}

// buildCfgWithDataDir creates a valid Config with the given DataDir for testing.
func buildCfgWithDataDir(dataDir string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Certbot.DataDir = dataDir
	return cfg
}

// findArgValue finds the value following a specific flag in an args slice.
func findArgValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// Feature: ux-improvements-batch1, Property 2: buildCertbotArgs Always Includes Directory Flags
// **Validates: Requirements 1.2**
func TestProperty_BuildCertbotArgsAlwaysIncludesDirFlags(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Generators that avoid the SuchThat/Map type panic issue
	domainLikeGen := gen.RegexMatch(`[a-z][a-z0-9]{1,10}\.[a-z]{2,5}`)
	emailLikeGen := gen.RegexMatch(`[a-z]{3,8}@[a-z]{3,8}\.[a-z]{2,4}`)
	dataDirChoiceGen := gen.OneConstOf("", "/custom/certbot", "./data/certbot", "/var/lib/certbot", "C:\\certbot\\data")

	// Property 1: buildCertbotArgs output always contains --config-dir, --work-dir, --logs-dir
	properties.Property("output always contains --config-dir, --work-dir, --logs-dir", prop.ForAll(
		func(domain string, email string, dataDir string) bool {
			cfg := config.DefaultConfig()
			cfg.Certbot = config.CertbotConfig{
				DataDir: dataDir,
				Email:   email,
			}
			runtimeCfg := config.NewRuntimeConfig(cfg)
			executor := &mockExecutor{}
			wrapper := NewCertbotWrapper(runtimeCfg, executor)

			args := wrapper.buildCertbotArgs([]string{domain}, email)

			hasConfigDir := false
			hasWorkDir := false
			hasLogsDir := false
			for _, arg := range args {
				switch arg {
				case "--config-dir":
					hasConfigDir = true
				case "--work-dir":
					hasWorkDir = true
				case "--logs-dir":
					hasLogsDir = true
				}
			}

			return hasConfigDir && hasWorkDir && hasLogsDir
		},
		domainLikeGen,
		emailLikeGen,
		dataDirChoiceGen,
	))

	// Property 2: --work-dir value is always effectiveDataDir()+"/work"
	properties.Property("--work-dir value is effectiveDataDir()/work", prop.ForAll(
		func(domain string, email string, dataDir string) bool {
			cfg := config.DefaultConfig()
			cfg.Certbot = config.CertbotConfig{
				DataDir: dataDir,
				Email:   email,
			}
			runtimeCfg := config.NewRuntimeConfig(cfg)
			executor := &mockExecutor{}
			wrapper := NewCertbotWrapper(runtimeCfg, executor)

			args := wrapper.buildCertbotArgs([]string{domain}, email)
			expectedWorkDir := filepath.Join(wrapper.effectiveDataDir(), "work")

			workDirVal := findArgValue(args, "--work-dir")
			return workDirVal == expectedWorkDir
		},
		domainLikeGen,
		emailLikeGen,
		dataDirChoiceGen,
	))

	// Property 3: --logs-dir value is always effectiveDataDir()+"/logs"
	properties.Property("--logs-dir value is effectiveDataDir()/logs", prop.ForAll(
		func(domain string, email string, dataDir string) bool {
			cfg := config.DefaultConfig()
			cfg.Certbot = config.CertbotConfig{
				DataDir: dataDir,
				Email:   email,
			}
			runtimeCfg := config.NewRuntimeConfig(cfg)
			executor := &mockExecutor{}
			wrapper := NewCertbotWrapper(runtimeCfg, executor)

			args := wrapper.buildCertbotArgs([]string{domain}, email)
			expectedLogsDir := filepath.Join(wrapper.effectiveDataDir(), "logs")

			logsDirVal := findArgValue(args, "--logs-dir")
			return logsDirVal == expectedLogsDir
		},
		domainLikeGen,
		emailLikeGen,
		dataDirChoiceGen,
	))

	properties.TestingRun(t)
}
