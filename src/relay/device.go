package relay

import (
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/localsend-monitor/src/protocol"
)

// DeviceTracker manages discovered devices and their online status
type DeviceTracker struct {
	mu       sync.RWMutex
	devices  map[string]*protocol.DeviceInfo
	timeout  time.Duration
	logger   *slog.Logger
	onJoin   func(device *protocol.DeviceInfo)
	onLeave  func(device *protocol.DeviceInfo)
	onUpdate func(device *protocol.DeviceInfo)
}

// NewDeviceTracker creates a new device tracker
func NewDeviceTracker(timeout time.Duration, logger *slog.Logger) *DeviceTracker {
	return &DeviceTracker{
		devices: make(map[string]*protocol.DeviceInfo),
		timeout: timeout,
		logger:  logger.With("component", "device-tracker"),
	}
}

// SetOnJoin sets the callback for when a device joins (first seen)
func (dt *DeviceTracker) SetOnJoin(fn func(device *protocol.DeviceInfo)) {
	dt.onJoin = fn
}

// SetOnLeave sets the callback for when a device leaves (timeout)
func (dt *DeviceTracker) SetOnLeave(fn func(device *protocol.DeviceInfo)) {
	dt.onLeave = fn
}

// SetOnUpdate sets the callback for when a device updates
func (dt *DeviceTracker) SetOnUpdate(fn func(device *protocol.DeviceInfo)) {
	dt.onUpdate = fn
}

// AddOrUpdate adds or updates a device in the tracker
func (dt *DeviceTracker) AddOrUpdate(msg *protocol.DiscoveryMessage, from *net.UDPAddr, ifaceName string, ifaceIP net.IP) *protocol.DeviceInfo {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	// Build device key
	key := msg.Fingerprint

	now := time.Now()

	existing, exists := dt.devices[key]
	if exists {
		// Update existing device
		existing.Alias = msg.Alias
		existing.Version = msg.Version
		existing.DeviceModel = msg.DeviceModel
		existing.DeviceType = msg.DeviceType
		existing.Port = msg.Port
		existing.Protocol = msg.Protocol
		existing.Download = msg.Download
		existing.IP = from.IP
		existing.SourceIface = ifaceName
		existing.LastSeen = now.Unix()
		existing.Online = true

		// If it was offline, treat as rejoin
		if !existing.Online {
			existing.Online = true
			if dt.onJoin != nil {
				dt.onJoin(existing)
			}
		} else if dt.onUpdate != nil {
			dt.onUpdate(existing)
		}

		return existing
	}

	// Create new device
	device := &protocol.DeviceInfo{
		Alias:       msg.Alias,
		Version:     msg.Version,
		DeviceModel: msg.DeviceModel,
		DeviceType:  msg.DeviceType,
		Fingerprint: msg.Fingerprint,
		Port:        msg.Port,
		Protocol:    msg.Protocol,
		Download:    msg.Download,
		IP:          from.IP,
		SourceIface: ifaceName,
		LastSeen:    now.Unix(),
		Online:      true,
	}

	// If no fingerprint, use IP:port as key
	if key == "" {
		key = device.Key()
	}

	dt.devices[key] = device

	if dt.onJoin != nil {
		dt.onJoin(device)
	}

	return device
}

// Remove removes a device from the tracker
func (dt *DeviceTracker) Remove(key string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if device, exists := dt.devices[key]; exists {
		device.Online = false
		if dt.onLeave != nil {
			dt.onLeave(device)
		}
		delete(dt.devices, key)
	}
}

// Get returns a device by key
func (dt *DeviceTracker) Get(key string) *protocol.DeviceInfo {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	return dt.devices[key]
}

// GetAll returns all tracked devices
func (dt *DeviceTracker) GetAll() []*protocol.DeviceInfo {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	result := make([]*protocol.DeviceInfo, 0, len(dt.devices))
	for _, device := range dt.devices {
		result = append(result, device)
	}
	return result
}

// GetByInterface returns all devices discovered on a specific interface
func (dt *DeviceTracker) GetByInterface(ifaceName string) []*protocol.DeviceInfo {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	var result []*protocol.DeviceInfo
	for _, device := range dt.devices {
		if device.MatchesInterface(ifaceName) {
			result = append(result, device)
		}
	}
	return result
}

// Count returns the number of tracked devices
func (dt *DeviceTracker) Count() int {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	return len(dt.devices)
}

// CleanupLoop periodically checks for offline devices and removes them
func (dt *DeviceTracker) CleanupLoop(interval time.Duration, ctxDone <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctxDone:
			return
		case <-ticker.C:
			dt.cleanup()
		}
	}
}

// cleanup removes devices that have timed out
func (dt *DeviceTracker) cleanup() {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	now := time.Now()
	for key, device := range dt.devices {
		lastSeen := time.Unix(device.LastSeen, 0)
		if now.Sub(lastSeen) > dt.timeout {
			device.Online = false
			if dt.onLeave != nil {
				dt.onLeave(device)
			}
			delete(dt.devices, key)
			dt.logger.Info("device removed due to timeout",
				"alias", device.Alias,
				"ip", device.IP.String(),
				"iface", device.SourceIface,
			)
		}
	}
}
