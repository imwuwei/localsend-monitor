package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/localsend-monitor/src/config"
	"github.com/localsend-monitor/src/forwarder"
	"github.com/localsend-monitor/src/protocol"
	"github.com/localsend-monitor/src/relay"
)

// Version info, set at build time
var (
	Version   = "dev"
	BuildTime = "unknown"
	Commit    = "none"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.json", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	listInterfaces := flag.Bool("list-interfaces", false, "List available network interfaces")
	flag.Parse()

	if *showVersion {
		fmt.Printf("localsend-monitor %s (build: %s, commit: %s)\n", Version, BuildTime, Commit)
		os.Exit(0)
	}

	if *listInterfaces {
		listNetworkInterfaces()
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Set up logging
	logger := setupLogger(cfg.LogLevel)

	logger.Info("starting localsend-monitor",
		"version", Version,
		"config", *configPath,
	)

	// Get network interfaces to use
	interfaces, err := config.GetInterfaces(cfg)
	if err != nil {
		logger.Error("failed to get network interfaces", "error", err)
		os.Exit(1)
	}

	// Filter out excluded interfaces (e.g., docker0, veth*, br-*)
	interfaces = filterExcludedInterfaces(interfaces, cfg.ExcludeInterfaces)

	if len(interfaces) == 0 {
		logger.Error("no suitable network interfaces found")
		os.Exit(1)
	}

	logger.Info("using network interfaces", "interfaces", interfaces)

	// Create the bridge
	bridgeCfg := relay.BridgeConfig{
		Interfaces:      interfaces,
		GroupAddr:       cfg.GroupAddr,
		Port:            cfg.Port,
		DeviceAlias:     cfg.DeviceAlias,
		Fingerprint:     cfg.Fingerprint,
		OfflineTimeout:  cfg.OfflineTimeout,
		CleanupInterval: cfg.CleanupInterval,
		ProxyEnabled:    cfg.ProxyEnabled,
		ProxyPort:       cfg.ProxyPort,
		ExcludeFP:       cfg.ExcludeFP,
	}

	bridge, err := relay.NewBridge(bridgeCfg, logger)
	if err != nil {
		logger.Error("failed to create bridge", "error", err)
		os.Exit(1)
	}

	// Create forwarder if enabled
	var fwd *forwarder.Forwarder
	if cfg.ForwarderEnabled {
		fwdCfg := forwarder.Config{
			ListenPort:       cfg.ForwarderPort,
			GroupAddr:        cfg.GroupAddr,
			Port:             cfg.Port,
			AnnounceInterval: cfg.CleanupInterval,
		}
		fwd = forwarder.NewForwarder(fwdCfg, logger)
	}

	// Set up API server
	var apiServer *APIServer
	if cfg.APIServerEnabled {
		apiServer = NewAPIServer(bridge, fwd, cfg, logger)
	}

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the bridge
	if err := bridge.Run(ctx); err != nil {
		logger.Error("failed to start bridge", "error", err)
		os.Exit(1)
	}

	// Start the forwarder
	if fwd != nil {
		if err := fwd.Start(ctx); err != nil {
			logger.Error("failed to start forwarder", "error", err)
		}
	}

	// Start the API server
	if apiServer != nil {
		go func() {
			if err := apiServer.Start(ctx); err != nil {
				logger.Error("API server error", "error", err)
			}
		}()
	}

	// Write status file if configured
	if cfg.StatusFile != "" {
		go writeStatusPeriodically(cfg.StatusFile, bridge, logger, ctx.Done())
	}

	logger.Info("localsend-monitor started successfully",
		"interfaces", interfaces,
		"proxy", cfg.ProxyEnabled,
		"forwarder", cfg.ForwarderEnabled,
		"api", cfg.APIServerEnabled,
	)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig)

	// Graceful shutdown
	bridge.Stop()
	if fwd != nil {
		fwd.Stop()
	}
	if apiServer != nil {
		apiServer.Stop()
	}

	logger.Info("localsend-monitor stopped")
}

// setupLogger creates a logger with the specified level
func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	return slog.New(handler)
}

// filterExcludedInterfaces removes interfaces that match the exclusion patterns
func filterExcludedInterfaces(interfaces, excludePatterns []string) []string {
	if len(excludePatterns) == 0 {
		return interfaces
	}

	var result []string
	for _, iface := range interfaces {
		excluded := false
		for _, pattern := range excludePatterns {
			// Support simple wildcard: "*" suffix matches any prefix
			if strings.HasSuffix(pattern, "*") {
				prefix := strings.TrimSuffix(pattern, "*")
				if strings.HasPrefix(iface, prefix) {
					excluded = true
					break
				}
			} else if strings.EqualFold(iface, pattern) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, iface)
		}
	}
	return result
}

// listNetworkInterfaces prints all available network interfaces
func listNetworkInterfaces() {
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list interfaces: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Available network interfaces:")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("%-10s %-20s %-15s %-10s %-10s\n", "Name", "MAC", "IP", "Flags", "MTU")
	fmt.Println(strings.Repeat("-", 80))

	for _, iface := range interfaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				flags := formatFlags(iface.Flags)
				fmt.Printf("%-10s %-20s %-15s %-10s %-10d\n",
					iface.Name, iface.HardwareAddr.String(), ipnet.IP.String(), flags, iface.MTU)
			}
		}
	}
}

// formatFlags formats interface flags
func formatFlags(flags net.Flags) string {
	var parts []string
	if flags&net.FlagUp != 0 {
		parts = append(parts, "UP")
	}
	if flags&net.FlagMulticast != 0 {
		parts = append(parts, "MCAST")
	}
	if flags&net.FlagBroadcast != 0 {
		parts = append(parts, "BCAST")
	}
	if flags&net.FlagLoopback != 0 {
		parts = append(parts, "LOOP")
	}
	return strings.Join(parts, ",")
}

// writeStatusPeriodically writes device status to a file
func writeStatusPeriodically(path string, bridge *relay.Bridge, logger *slog.Logger, done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			writeStatusFile(path, bridge, logger)
		}
	}
}

// writeStatusFile writes the current status to a JSON file
func writeStatusFile(path string, bridge *relay.Bridge, logger *slog.Logger) {
	devices := bridge.GetTracker().GetAll()
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Alias < devices[j].Alias
	})

	status := struct {
		Timestamp int64                  `json:"timestamp"`
		Devices   []*protocol.DeviceInfo `json:"devices"`
		Count     int                    `json:"count"`
	}{
		Timestamp: time.Now().Unix(),
		Devices:   devices,
		Count:     len(devices),
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		logger.Error("failed to marshal status", "error", err)
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		logger.Error("failed to write status file", "error", err)
	}
}
