package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	interfacesStr := flag.String("interfaces", "", "Network interfaces to listen on (comma-separated)")
	interfacesStrShort := flag.String("i", "", "Network interfaces to listen on (comma-separated, shorthand)")
	listInterfaces := flag.Bool("list-interfaces", false, "List available network interfaces")
	listInterfacesShort := flag.Bool("L", false, "List available network interfaces (shorthand)")
	showVersion := flag.Bool("version", false, "Show version information")
	showVersionShort := flag.Bool("v", false, "Show version information (shorthand)")
	flag.Parse()

	if *showVersion || *showVersionShort {
		fmt.Printf("localsend-monitor %s (build: %s, commit: %s)\n", Version, BuildTime, Commit)
		os.Exit(0)
	}

	if *listInterfaces || *listInterfacesShort {
		listNetworkInterfaces()
		os.Exit(0)
	}

	// Get interfaces from flag (short form takes precedence if both provided)
	ifaces := *interfacesStr
	if *interfacesStrShort != "" {
		ifaces = *interfacesStrShort
	}

	if ifaces == "" {
		fmt.Fprintln(os.Stderr, "Error: no interfaces specified. Use -L or -list-interfaces to see available interfaces.")
		os.Exit(1)
	}

	// Parse comma-separated interfaces
	interfaces := strings.Split(ifaces, ",")
	for i, iface := range interfaces {
		interfaces[i] = strings.TrimSpace(iface)
	}

	// Set up logging with default level
	logger := setupLogger("info")

	logger.Info("starting localsend-monitor",
		"version", Version,
		"interfaces", interfaces,
	)

	// Build bridge config with hardcoded defaults
	bridgeCfg := relay.BridgeConfig{
		Interfaces:      interfaces,
		GroupAddr:       "224.0.0.167",
		Port:            53317,
		DeviceAlias:     "",
		Fingerprint:     "",
		OfflineTimeout:  5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		ProxyEnabled:    false,
		ProxyPort:       53317,
		ExcludeFP:       nil,
	}

	bridge, err := relay.NewBridge(bridgeCfg, logger)
	if err != nil {
		logger.Error("failed to create bridge", "error", err)
		os.Exit(1)
	}

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the bridge
	if err := bridge.Run(ctx); err != nil {
		logger.Error("failed to start bridge", "error", err)
		os.Exit(1)
	}

	logger.Info("localsend-monitor started successfully",
		"interfaces", interfaces,
	)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig)

	// Graceful shutdown
	bridge.Stop()

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
