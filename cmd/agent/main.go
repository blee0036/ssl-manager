package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/agent/cli"
	agentconfig "github.com/ssl-manager/ssl-manager/internal/agent/config"
	"github.com/ssl-manager/ssl-manager/internal/agent/platform"
	"github.com/ssl-manager/ssl-manager/internal/agent/updater"
	"github.com/ssl-manager/ssl-manager/internal/agent/version"
	"github.com/ssl-manager/ssl-manager/internal/agent/worker"
)

// Version and BuildTime are set at build time via ldflags:
//
//	go build -ldflags "-X main.Version=1.2.3 -X main.BuildTime=2024-01-01T00:00:00Z"
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		arg := os.Args[1]

		// Handle global flags
		if arg == "--help" || arg == "-h" {
			printUsage()
			os.Exit(0)
			return
		}
		if arg == "--version" || arg == "-v" {
			cmdVersion()
			return
		}

		// If first arg doesn't start with -, treat as subcommand
		if !strings.HasPrefix(arg, "-") {
			switch arg {
			case "version":
				cmdVersion()
			case "uninstall":
				cmdUninstall(os.Args[2:])
			case "restart":
				cmdRestart()
			case "logs":
				cmdLogs(os.Args[2:])
			case "update":
				cmdUpdate()
			case "auto-update":
				cmdAutoUpdate(os.Args[2:])
			case "config":
				cmdConfig(os.Args[2:])
			case "help":
				printUsage()
			default:
				fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", arg)
				printUsage()
				os.Exit(1)
			}
			return
		}
	}
	// No subcommand → start daemon (existing logic)
	runDaemon()
}

// printUsage outputs help information listing all available subcommands.
func printUsage() {
	fmt.Println("Usage: ssl-manager-agent [command]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  version      显示版本信息")
	fmt.Println("  update       检查并更新到最新版本")
	fmt.Println("  auto-update  查看或设置自动更新状态 (enable/disable)")
	fmt.Println("  restart      重启 Agent 服务")
	fmt.Println("  logs         查看 Agent 日志 (--follow, --lines N)")
	fmt.Println("  config       查看或修改配置 (--server-url, --token, --interactive)")
	fmt.Println("  uninstall    完整卸载 Agent (--yes 跳过确认)")
	fmt.Println("  help         显示此帮助信息")
	fmt.Println()
	fmt.Println("不带命令时启动 Agent 守护进程。")
}

// cmdVersion displays version, build time, and platform information.
func cmdVersion() {
	fmt.Printf("ssl-manager-agent version %s\n", Version)
	fmt.Printf("  Build time: %s\n", BuildTime)
	fmt.Printf("  OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// cmdUninstall performs a complete uninstall of the Agent.
// It stops and disables the service, removes service files, config directory, and binary.
func cmdUninstall(args []string) {
	// Parse --yes flag
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	fs.Parse(args)

	// Prompt for confirmation unless --yes is provided
	if !*yes {
		fmt.Print("Are you sure you want to uninstall? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Uninstall cancelled.")
			return
		}
	}

	// Get platform-specific service manager
	svcMgr := platform.NewServiceManager()
	if svcMgr == nil {
		fmt.Fprintf(os.Stderr, "Error: unsupported platform %s\n", runtime.GOOS)
		os.Exit(1)
	}

	var deleted []string
	var errs []error

	// Stop the service
	fmt.Println("Stopping service...")
	if err := svcMgr.Stop(); err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
			os.Exit(1)
		}
		// Service may not be running, continue
		fmt.Printf("  Warning: failed to stop service: %v\n", err)
	}

	// Disable the service
	fmt.Println("Disabling service...")
	if err := svcMgr.Disable(); err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
			os.Exit(1)
		}
		fmt.Printf("  Warning: failed to disable service: %v\n", err)
	}

	// Uninstall service files (unit file / plist + daemon-reload/bootout + log files on macOS)
	fmt.Println("Removing service files...")
	if err := svcMgr.Uninstall(); err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
			os.Exit(1)
		}
		errs = append(errs, fmt.Errorf("uninstall service: %w", err))
	} else {
		deleted = append(deleted, "service files")
	}

	// Delete config directory (uses platform-specific path from config package)
	configDir := agentconfig.DefaultConfigDir
	if configDir != "" {
		fmt.Printf("Removing config directory: %s\n", configDir)
		if err := os.RemoveAll(configDir); err != nil {
			if isPermissionError(err) {
				fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
				os.Exit(1)
			}
			errs = append(errs, fmt.Errorf("remove config dir: %w", err))
		} else {
			deleted = append(deleted, configDir)
		}
	}

	// Delete binary file
	binaryPath := "/usr/local/bin/ssl-manager-agent"
	fmt.Printf("Removing binary: %s\n", binaryPath)
	if err := os.Remove(binaryPath); err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
			os.Exit(1)
		}
		if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove binary: %w", err))
		}
	} else {
		deleted = append(deleted, binaryPath)
	}

	// Print summary
	fmt.Println()
	fmt.Println("=== Uninstall Summary ===")
	if len(deleted) > 0 {
		fmt.Println("Deleted:")
		for _, item := range deleted {
			fmt.Printf("  - %s\n", item)
		}
	}
	if len(errs) > 0 {
		fmt.Println("Errors:")
		for _, err := range errs {
			fmt.Printf("  - %s\n", err)
		}
	}
	if len(errs) == 0 {
		fmt.Println("Uninstall completed successfully.")
	}
}

// isPermissionError checks if an error is a permission denied error.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	return strings.Contains(err.Error(), "permission denied")
}

// cmdRestart restarts the Agent service via the platform service manager.
func cmdRestart() {
	svcMgr := platform.NewServiceManager()
	if svcMgr == nil {
		fmt.Fprintf(os.Stderr, "Error: unsupported platform %s\n", runtime.GOOS)
		os.Exit(1)
	}

	fmt.Println("Restarting ssl-manager-agent service...")
	if err := svcMgr.Restart(); err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: failed to restart service: %v\n", err)
		os.Exit(1)
	}

	// Check and display service status
	active, err := svcMgr.IsActive()
	if err != nil {
		fmt.Println("Service restarted, but unable to verify status.")
		return
	}
	if active {
		fmt.Println("Service restarted successfully. Status: active")
	} else {
		fmt.Println("Service restart command completed, but service is not active.")
	}
}

// cmdLogs displays Agent service logs with optional follow and line count flags.
func cmdLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("follow", false, "stream logs in real-time")
	lines := fs.Int("lines", 50, "number of log lines to display")
	fs.Parse(args)

	svcMgr := platform.NewServiceManager()
	if svcMgr == nil {
		fmt.Fprintf(os.Stderr, "Error: unsupported platform %s\n", runtime.GOOS)
		os.Exit(1)
	}

	if err := svcMgr.GetLogs(*lines, *follow); err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: failed to get logs: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: ensure the service has been started at least once.\n")
		os.Exit(1)
	}
}

// cmdUpdate checks for a newer Agent version and performs the update if available.
func cmdUpdate() {
	// Load config to get ServerURL
	cfg, err := agentconfig.LoadConfig(agentconfig.DefaultConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config path: %s\n", agentconfig.DefaultConfigPath)
		os.Exit(1)
	}

	// Get current executable path
	currentPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to determine current binary path: %v\n", err)
		os.Exit(1)
	}

	// Create Updater
	u := &updater.Updater{
		ServerURL:   cfg.ServerURL,
		CurrentPath: currentPath,
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}

	// Query latest version
	fmt.Println("Checking for updates...")
	info, err := u.CheckVersion(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to check for updates: %v\n", err)
		fmt.Fprintf(os.Stderr, "Server: %s\n", cfg.ServerURL)
		os.Exit(1)
	}

	if info == nil {
		fmt.Fprintf(os.Stderr, "Error: no release found for %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(1)
	}

	// Compare versions
	newer, err := version.IsNewer(Version, info.Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: version comparison failed: %v\n", err)
		os.Exit(1)
	}

	if !newer {
		fmt.Printf("Agent is already up to date (version %s)\n", Version)
		return
	}

	// New version available - perform update
	fmt.Printf("Updating from %s to %s...\n", Version, info.Version)

	// Download
	tmpPath, err := u.Download(info.DownloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: download failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Server: %s\n", cfg.ServerURL)
		os.Exit(1)
	}

	// Verify MD5
	if err := updater.VerifyMD5(tmpPath, info.MD5); err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "Error: MD5 verification failed: %v\n", err)
		os.Exit(1)
	}

	// Atomic replace
	if err := updater.AtomicReplace(currentPath, tmpPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to replace binary: %v\n", err)
		os.Exit(1)
	}

	// Restart service
	svcMgr := platform.NewServiceManager()
	if svcMgr == nil {
		fmt.Printf("Updated from %s to %s successfully.\n", Version, info.Version)
		fmt.Println("Warning: unsupported platform, please restart the service manually.")
		return
	}

	if err := svcMgr.Restart(); err != nil {
		fmt.Printf("Updated from %s to %s successfully.\n", Version, info.Version)
		fmt.Fprintf(os.Stderr, "Warning: failed to restart service: %v\n", err)
		fmt.Println("Please restart the service manually.")
		return
	}

	fmt.Printf("Updated from %s to %s successfully.\n", Version, info.Version)
	fmt.Println("Service restarted.")
}

// cmdAutoUpdate displays or changes the auto-update configuration.
func cmdAutoUpdate(args []string) {
	configPath := agentconfig.DefaultConfigPath

	cfg, err := agentconfig.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config path: %s\n", configPath)
		os.Exit(1)
	}

	// No args: display current status
	if len(args) == 0 {
		if cfg.IsAutoUpdateEnabled() {
			fmt.Println("Auto-update: enabled")
		} else {
			fmt.Println("Auto-update: disabled")
		}
		return
	}

	switch args[0] {
	case "enable":
		val := true
		cfg.AutoUpdate = &val
	case "disable":
		val := false
		cfg.AutoUpdate = &val
	default:
		fmt.Fprintf(os.Stderr, "Unknown argument: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "Usage: ssl-manager-agent auto-update [enable|disable]\n")
		os.Exit(1)
	}

	if err := agentconfig.SaveConfig(configPath, cfg); err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: failed to save config: %v\n", err)
		os.Exit(1)
	}

	if cfg.IsAutoUpdateEnabled() {
		fmt.Println("Auto-update has been enabled.")
	} else {
		fmt.Println("Auto-update has been disabled.")
	}
}

// cmdConfig displays or modifies the Agent configuration.
func cmdConfig(args []string) {
	configPath := agentconfig.DefaultConfigPath

	fs := flag.NewFlagSet("config", flag.ExitOnError)
	serverURL := fs.String("server-url", "", "set server URL")
	token := fs.String("token", "", "set agent token")
	interactive := fs.Bool("interactive", false, "interactive configuration mode")
	fs.Parse(args)

	cfg, err := agentconfig.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config path: %s\n", configPath)
		os.Exit(1)
	}

	// Interactive mode
	if *interactive {
		reader := bufio.NewReader(os.Stdin)

		fmt.Printf("Server URL [%s]: ", cfg.ServerURL)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "" {
			if !cli.ValidateURL(input) {
				fmt.Fprintf(os.Stderr, "Error: invalid URL. Must start with http:// or https://\n")
				os.Exit(1)
			}
			cfg.ServerURL = input
		}

		fmt.Printf("Agent Token [%s]: ", cli.MaskToken(cfg.AgentToken))
		input, _ = reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "" {
			cfg.AgentToken = input
		}

		if err := agentconfig.SaveConfig(configPath, cfg); err != nil {
			if isPermissionError(err) {
				fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Error: failed to save config: %v\n", err)
			fmt.Fprintf(os.Stderr, "Config path: %s\n", configPath)
			os.Exit(1)
		}

		fmt.Println("Configuration updated:")
		fmt.Printf("  server_url:  %s\n", cfg.ServerURL)
		fmt.Printf("  agent_token: %s\n", cli.MaskToken(cfg.AgentToken))
		fmt.Println()
		fmt.Println("Hint: run 'ssl-manager-agent restart' to apply changes.")
		return
	}

	// Flag-based updates
	modified := false

	if *serverURL != "" {
		if !cli.ValidateURL(*serverURL) {
			fmt.Fprintf(os.Stderr, "Error: invalid URL. Must start with http:// or https://\n")
			os.Exit(1)
		}
		cfg.ServerURL = *serverURL
		modified = true
	}

	if *token != "" {
		cfg.AgentToken = *token
		modified = true
	}

	if modified {
		if err := agentconfig.SaveConfig(configPath, cfg); err != nil {
			if isPermissionError(err) {
				fmt.Fprintf(os.Stderr, "Error: permission denied. Please run with sudo.\n")
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Error: failed to save config: %v\n", err)
			fmt.Fprintf(os.Stderr, "Config path: %s\n", configPath)
			os.Exit(1)
		}

		fmt.Println("Configuration updated:")
		fmt.Printf("  server_url:  %s\n", cfg.ServerURL)
		fmt.Printf("  agent_token: %s\n", cli.MaskToken(cfg.AgentToken))
		fmt.Println()
		fmt.Println("Hint: run 'ssl-manager-agent restart' to apply changes.")
		return
	}

	// No flags: display current config
	fmt.Println("Current configuration:")
	fmt.Printf("  server_url:           %s\n", cfg.ServerURL)
	fmt.Printf("  machine_id:           %s\n", cfg.MachineID)
	fmt.Printf("  agent_token:          %s\n", cli.MaskToken(cfg.AgentToken))
	fmt.Printf("  poll_interval_seconds: %d\n", cfg.PollIntervalSeconds)
	fmt.Printf("  log_level:            %s\n", cfg.LogLevel)
	if cfg.IsAutoUpdateEnabled() {
		fmt.Printf("  auto_update:          enabled\n")
	} else {
		fmt.Printf("  auto_update:          disabled\n")
	}
	fmt.Printf("\nConfig path: %s\n", configPath)
}

// runDaemon starts the Agent daemon (existing behavior, backward compatible).
func runDaemon() {
	log.Println("[INFO] SSL Manager Agent starting...")

	// Set the worker package AgentVersion from the compile-time injected Version.
	worker.AgentVersion = Version

	if err := runDaemonMain(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runDaemonMain() error {
	configPath := flag.String("config", agentconfig.DefaultConfigPath, "path to agent config file")
	statePath := flag.String("state", agentconfig.DefaultStatePath, "path to agent state file")
	flag.Parse()

	// Load configuration
	cfg, err := agentconfig.LoadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	log.Printf("[INFO] Config loaded: server_url=%s, machine_id=%s, poll_interval=%ds",
		cfg.ServerURL, cfg.MachineID, cfg.PollIntervalSeconds)

	// Load local state (recovers from previous run)
	state, err := agentconfig.LoadState(*statePath)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}
	log.Printf("[INFO] State loaded: %d certificate states tracked",
		len(state.MachineCertStates))

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start heartbeat worker
	heartbeatWorker := worker.NewHeartbeatWorker(cfg)
	tokenRevokedCh := heartbeatWorker.Run(ctx)

	// Start sync worker
	syncW := worker.NewSyncWorker(cfg, state)
	deployW := worker.NewDeployWorker(cfg, state, *statePath)
	go syncLoop(ctx, cfg, syncW, deployW, state, *statePath)

	log.Println("[INFO] Agent is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal or token revocation
	select {
	case <-sigCh:
		log.Println("[INFO] Shutdown signal received, stopping agent...")
	case <-tokenRevokedCh:
		log.Println("[INFO] Agent token revoked, stopping agent...")
	}
	cancel()

	// Save state before exit
	if err := agentconfig.SaveState(*statePath, state); err != nil {
		log.Printf("[ERROR] Failed to save state on shutdown: %v", err)
	} else {
		log.Println("[INFO] State saved successfully")
	}

	log.Println("[INFO] Agent stopped")
	return nil
}

// syncLoop periodically syncs certificate configurations and triggers deployments.
func syncLoop(ctx context.Context, cfg *agentconfig.AgentConfig, syncW *worker.SyncWorker, deployW *worker.DeployWorker, state *agentconfig.AgentLocalState, statePath string) {
	// Wait a short delay before first sync to allow heartbeat to establish connection
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	log.Println("[INFO] Starting certificate sync worker...")

	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[INFO] Sync worker stopped")
			return
		case <-ticker.C:
			syncAndDeploy(ctx, syncW, deployW, state, statePath)
		}
	}
}

// syncAndDeploy pulls certificate configs and triggers deployments as needed.
func syncAndDeploy(ctx context.Context, syncW *worker.SyncWorker, deployW *worker.DeployWorker, state *agentconfig.AgentLocalState, statePath string) {
	configs, err := syncW.GetConfigsNeedingDeployment(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to sync certificate configs: %v", err)
		return
	}

	if len(configs) == 0 {
		log.Println("[DEBUG] No certificates need deployment")
		return
	}

	log.Printf("[INFO] %d certificate(s) need deployment", len(configs))

	for _, cfg := range configs {
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Printf("[INFO] Deploying certificate %s (machine_cert_id=%s, revision=%d)",
			cfg.CertificateID, cfg.MachineCertificateID, cfg.ConfigRevision)

		result, err := deployW.Deploy(ctx, cfg)
		if err != nil {
			log.Printf("[ERROR] Deployment failed for %s: %v", cfg.MachineCertificateID, err)
			continue
		}

		if result.Status == "success" {
			log.Printf("[INFO] Successfully deployed certificate %s to %s",
				cfg.CertificateID, cfg.CertPath)
		} else {
			log.Printf("[WARN] Deployment completed with status=%s for %s: %s",
				result.Status, cfg.MachineCertificateID, result.ErrorMessage)
		}
	}

	// Save state after deployments
	if err := agentconfig.SaveState(statePath, state); err != nil {
		log.Printf("[ERROR] Failed to save state after sync: %v", err)
	}
}
