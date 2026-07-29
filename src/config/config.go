package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Config holds the application configuration
type Config struct {
	// Network interfaces to listen on (empty = auto-detect)
	Interfaces []string `json:"interfaces"`

	// Interfaces to exclude from auto-detection (e.g., ["docker0", "veth*"])
	ExcludeInterfaces []string `json:"excludeInterfaces"`

	// Multicast configuration
	GroupAddr string `json:"groupAddr"`
	Port      int    `json:"port"`

	// Device identity
	DeviceAlias string `json:"deviceAlias"`
	Fingerprint string `json:"fingerprint"`

	// Device timeout settings
	OfflineTimeout  time.Duration `json:"offlineTimeout"`
	CleanupInterval time.Duration `json:"cleanupInterval"`

	// HTTP proxy settings
	ProxyEnabled bool `json:"proxyEnabled"`
	ProxyPort    int  `json:"proxyPort"`

	// Forwarding settings
	ForwarderEnabled bool `json:"forwarderEnabled"`
	ForwarderPort    int  `json:"forwarderPort"`

	// Logging
	LogLevel string `json:"logLevel"` // "debug", "info", "warn", "error"

	// Exclude fingerprints (prevent self-discovery loops)
	ExcludeFP []string `json:"excludeFP"`

	// API server settings
	APIServerEnabled bool   `json:"apiServerEnabled"`
	APIServerPort    int    `json:"apiServerPort"`
	APIListenAddr    string `json:"apiListenAddr"`

	// Status file for external monitoring
	StatusFile string `json:"statusFile"`

	// Config file path
	ConfigPath string `json:"-"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		ExcludeInterfaces: []string{"docker0", "veth*", "br-*", "tun*"},
		GroupAddr:         "224.0.0.167",
		Port:              53317,
		OfflineTimeout:    5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
		ProxyEnabled:      false,
		ProxyPort:         53317,
		ForwarderEnabled:  false,
		ForwarderPort:     53318,
		LogLevel:          "info",
		APIServerEnabled:  true,
		APIServerPort:     8080,
		APIListenAddr:     "0.0.0.0",
	}
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	cfg.ConfigPath = path

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// SaveConfig saves the configuration to a JSON file
func SaveConfig(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetInterfaces returns the list of interfaces to use
// If empty, auto-detects all non-loopback interfaces
func GetInterfaces(cfg *Config) ([]string, error) {
	if len(cfg.Interfaces) > 0 {
		return cfg.Interfaces, nil
	}

	// Auto-detect interfaces
	ifaces, err := osInterfaces()
	if err != nil {
		return nil, err
	}

	return ifaces, nil
}

// osInterfaces returns all non-loopback, up, multicast-capable interfaces
func osInterfaces() ([]string, error) {
	interfaces, err := osInterfacesList()
	if err != nil {
		return nil, err
	}

	var result []string
	for _, iface := range interfaces {
		// Skip loopback
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Skip if not up
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Check if we have an IPv4 address
		addrs, _ := iface.Addrs()
		hasIPv4 := false
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				hasIPv4 = true
				break
			}
		}

		if hasIPv4 {
			result = append(result, iface.Name)
		}
	}

	return result, nil
}

// osInterfacesList is a wrapper for net.Interfaces for testing
var osInterfacesList = func() ([]net.Interface, error) {
	return net.Interfaces()
}
