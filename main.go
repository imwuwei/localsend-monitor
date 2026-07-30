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

func init() {
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "用法: %s [选项]\n\n", flag.CommandLine.Name())
		fmt.Fprintf(out, "选项:\n")
		fmt.Fprintf(out, "  -i, --interfaces <string>       要监听的网络接口（逗号分隔）\n")
		fmt.Fprintf(out, "  -g, --group-addr <string>       多播组地址（默认: 224.0.0.167）\n")
		fmt.Fprintf(out, "  -p, --port <int>                多播端口（默认: 53317）\n")
		fmt.Fprintf(out, "  -t, --offline-timeout <duration> 设备离线超时时间（默认: 5m）\n")
		fmt.Fprintf(out, "  -c, --cleanup-interval <duration> 清理间隔（默认: 1m）\n")
		fmt.Fprintf(out, "      --exclude-fp <string>        排除的指纹列表（逗号分隔）\n")
		fmt.Fprintf(out, "  -L, --list-interfaces           列出可用的网络接口\n")
		fmt.Fprintf(out, "  -v, --version                   显示版本信息\n")
		fmt.Fprintf(out, "  -h, --help                      显示帮助信息\n")
	}
}

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
	groupAddr := flag.String("group-addr", "224.0.0.167", "Multicast group address")
	groupAddrShort := flag.String("g", "224.0.0.167", "Multicast group address (shorthand)")
	port := flag.Int("port", 53317, "Multicast port")
	portShort := flag.Int("p", 53317, "Multicast port (shorthand)")
	offlineTimeout := flag.Duration("offline-timeout", 5*time.Minute, "Device offline timeout")
	offlineTimeoutShort := flag.Duration("t", 5*time.Minute, "Device offline timeout (shorthand)")
	cleanupInterval := flag.Duration("cleanup-interval", 1*time.Minute, "Cleanup interval")
	cleanupIntervalShort := flag.Duration("c", 1*time.Minute, "Cleanup interval (shorthand)")
	excludeFPStr := flag.String("exclude-fp", "", "Fingerprints to exclude (comma-separated)")
	listInterfaces := flag.Bool("list-interfaces", false, "List available network interfaces")
	listInterfacesShort := flag.Bool("L", false, "List available network interfaces (shorthand)")
	showVersion := flag.Bool("version", false, "Show version information")
	showVersionShort := flag.Bool("v", false, "Show version information (shorthand)")
	showHelp := flag.Bool("h", false, "Show help")
	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

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

	// Short form takes precedence for all configurable options
	multicastAddr := *groupAddr
	if *groupAddrShort != "224.0.0.167" {
		multicastAddr = *groupAddrShort
	}
	multicastPort := *port
	if *portShort != 53317 {
		multicastPort = *portShort
	}
	offTimeout := *offlineTimeout
	if *offlineTimeoutShort != 5*time.Minute {
		offTimeout = *offlineTimeoutShort
	}
	cleanInterval := *cleanupInterval
	if *cleanupIntervalShort != 1*time.Minute {
		cleanInterval = *cleanupIntervalShort
	}

	// Parse comma-separated excluded fingerprints
	var excludeFP []string
	if *excludeFPStr != "" {
		parts := strings.Split(*excludeFPStr, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				excludeFP = append(excludeFP, p)
			}
		}
	}

	// Set up logging with default level
	logger := setupLogger("info")

	logger.Info("starting localsend-monitor",
		"version", Version,
		"interfaces", interfaces,
		"group_addr", multicastAddr,
		"port", multicastPort,
		"offline_timeout", offTimeout.String(),
		"cleanup_interval", cleanInterval.String(),
	)

	// Build bridge config from command-line flags
	bridgeCfg := relay.BridgeConfig{
		Interfaces:      interfaces,
		GroupAddr:       multicastAddr,
		Port:            multicastPort,
		OfflineTimeout:  offTimeout,
		CleanupInterval: cleanInterval,
		ExcludeFP:       excludeFP,
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
