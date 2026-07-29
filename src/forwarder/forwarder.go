package forwarder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/localsend-monitor/src/protocol"
)

// Forwarder actively forwards multicast discovery messages between subnets.
// Unlike the Bridge which does real-time forwarding, the Forwarder can
// periodically re-broadcast discovery messages from one subnet to another.
type Forwarder struct {
	config    Config
	logger    *slog.Logger
	mu        sync.RWMutex
	devices   map[string]*DeviceRoute
	server    *http.Server
	startTime time.Time
}

// Config holds the forwarder configuration
type Config struct {
	ListenPort       int
	TargetSubnets    []string // Target subnet CIDRs (e.g., ["192.168.2.0/24"])
	GroupAddr        string
	Port             int
	AnnounceInterval time.Duration
	BroadcastAddr    string // Optional broadcast address for subnet
}

// DeviceRoute represents a known device and how to reach it
type DeviceRoute struct {
	Device   *protocol.DeviceInfo
	LastSeen time.Time
	Subnet   string
}

// NewForwarder creates a new forwarder
func NewForwarder(cfg Config, logger *slog.Logger) *Forwarder {
	if cfg.GroupAddr == "" {
		cfg.GroupAddr = protocol.DefaultMulticastAddr
	}
	if cfg.Port == 0 {
		cfg.Port = protocol.DefaultPort
	}
	if cfg.AnnounceInterval == 0 {
		cfg.AnnounceInterval = 30 * time.Second
	}

	return &Forwarder{
		config:  cfg,
		logger:  logger.With("component", "forwarder"),
		devices: make(map[string]*DeviceRoute),
	}
}

// Start starts the forwarder
func (f *Forwarder) Start(ctx context.Context) error {
	f.startTime = time.Now()

	// Start HTTP server for receiving forwarded device lists
	if f.config.ListenPort > 0 {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/localsend/v2/forwarded-devices", f.handleForwardedDevices)
		mux.HandleFunc("/health", f.handleHealth)

		f.server = &http.Server{
			Addr:    fmt.Sprintf(":%d", f.config.ListenPort),
			Handler: mux,
		}

		go func() {
			f.logger.Info("forwarder server starting", "port", f.config.ListenPort)
			if err := f.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				f.logger.Error("forwarder server error", "error", err)
			}
		}()
	}

	// Start periodic announcement for each target subnet
	if len(f.config.TargetSubnets) > 0 {
		go f.announcementLoop(ctx)
	}

	f.logger.Info("forwarder started",
		"target_subnets", f.config.TargetSubnets,
		"listen_port", f.config.ListenPort,
	)

	return nil
}

// AnnounceDevice sends a discovery announcement to all target subnets
func (f *Forwarder) AnnounceDevice(device *protocol.DeviceInfo) error {
	// Build a discovery message for this device
	msg := protocol.DiscoveryMessage{
		Alias:       device.Alias,
		Version:     device.Version,
		DeviceModel: device.DeviceModel,
		DeviceType:  device.DeviceType,
		Fingerprint: device.Fingerprint,
		Port:        device.Port,
		Protocol:    device.Protocol,
		Download:    device.Download,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal discovery message: %w", err)
	}

	// Send to each target subnet via broadcast
	for _, subnet := range f.config.TargetSubnets {
		if err := f.sendToSubnet(subnet, data); err != nil {
			f.logger.Warn("failed to send to subnet",
				"subnet", subnet,
				"device", device.Alias,
				"error", err,
			)
		}
	}

	return nil
}

// AnnounceDeviceList sends all known devices to the forwarder server on another instance
func (f *Forwarder) AnnounceDeviceList(devices []*protocol.DeviceInfo, targetAddr string) error {
	payload := struct {
		Devices []*protocol.DeviceInfo `json:"devices"`
		Source  string                 `json:"source"`
		Time    int64                  `json:"time"`
	}{
		Devices: devices,
		Source:  f.getLocalIP(),
		Time:    time.Now().Unix(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal device list: %w", err)
	}

	url := fmt.Sprintf("http://%s/api/localsend/v2/forwarded-devices", targetAddr)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to send device list: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// AddDevice adds a device to the forwarder's routing table
func (f *Forwarder) AddDevice(device *protocol.DeviceInfo, subnet string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := device.Key()
	f.devices[key] = &DeviceRoute{
		Device:   device,
		LastSeen: time.Now(),
		Subnet:   subnet,
	}
}

// GetDevices returns all known devices
func (f *Forwarder) GetDevices() []*protocol.DeviceInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]*protocol.DeviceInfo, 0, len(f.devices))
	for _, route := range f.devices {
		result = append(result, route.Device)
	}
	return result
}

// sendToSubnet sends data to all devices in a subnet
func (f *Forwarder) sendToSubnet(subnet string, data []byte) error {
	// Parse the subnet CIDR
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return fmt.Errorf("invalid subnet %s: %w", subnet, err)
	}

	// Get the broadcast address for this subnet
	broadcast := f.getBroadcastAddr(ipnet)
	if broadcast == nil {
		return fmt.Errorf("cannot determine broadcast address for %s", subnet)
	}

	// Send to multicast group
	addr := &net.UDPAddr{
		IP:   net.ParseIP(f.config.GroupAddr),
		Port: f.config.Port,
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to create UDP connection: %w", err)
	}
	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write(data)
	return err
}

// getBroadcastAddr calculates the broadcast address from a subnet
func (f *Forwarder) getBroadcastAddr(ipnet *net.IPNet) net.IP {
	ip := ipnet.IP.To4()
	if ip == nil {
		return nil
	}

	mask := ipnet.Mask
	broadcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		broadcast[i] = ip[i] | ^mask[i]
	}
	return broadcast
}

// getLocalIP returns the local IP address of this machine
func (f *Forwarder) getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "unknown"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// announcementLoop periodically announces devices to target subnets
func (f *Forwarder) announcementLoop(ctx context.Context) {
	ticker := time.NewTicker(f.config.AnnounceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.announceKnownDevices()
		}
	}
}

// announceKnownDevices announces all known devices to target subnets
func (f *Forwarder) announceKnownDevices() {
	devices := f.GetDevices()
	if len(devices) == 0 {
		return
	}

	f.logger.Info("announcing devices to target subnets",
		"count", len(devices),
		"subnets", f.config.TargetSubnets,
	)

	for _, device := range devices {
		if err := f.AnnounceDevice(device); err != nil {
			f.logger.Warn("failed to announce device",
				"alias", device.Alias,
				"error", err,
			)
		}
	}
}

// handleForwardedDevices handles incoming device lists from other forwarder instances
func (f *Forwarder) handleForwardedDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Devices []*protocol.DeviceInfo `json:"devices"`
		Source  string                 `json:"source"`
		Time    int64                  `json:"time"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	f.logger.Info("received forwarded devices",
		"count", len(payload.Devices),
		"source", payload.Source,
	)

	for _, device := range payload.Devices {
		f.AddDevice(device, payload.Source)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleHealth handles health check requests
func (f *Forwarder) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"uptime":  time.Since(f.startTime).String(),
		"devices": len(f.devices),
		"subnets": f.config.TargetSubnets,
	})
}

// Stop stops the forwarder
func (f *Forwarder) Stop() {
	if f.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		f.server.Shutdown(ctx)
	}
	f.logger.Info("forwarder stopped")
}
