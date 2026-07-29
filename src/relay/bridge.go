package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/localsend-monitor/src/multicast"
	"github.com/localsend-monitor/src/protocol"
)

// Bridge is the core component that bridges multicast messages between interfaces
type Bridge struct {
	listeners   []*multicast.Listener
	senders     map[string]*multicast.Sender
	tracker     *DeviceTracker
	proxy       *Proxy
	logger      *slog.Logger
	deviceAlias string
	fingerprint string
	msgChans    []chan *multicast.Message
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.RWMutex
	excludedFP  []string      // fingerprints to exclude (self-discovery prevention)
	dedup       *messageDedup // message deduplication to prevent loops
}

// BridgeConfig holds configuration for the bridge
type BridgeConfig struct {
	Interfaces      []string
	GroupAddr       string
	Port            int
	DeviceAlias     string
	Fingerprint     string
	OfflineTimeout  time.Duration
	CleanupInterval time.Duration
	ProxyEnabled    bool
	ProxyPort       int
	ExcludeFP       []string
}

// messageDedup provides short-term message deduplication to prevent forwarding loops
type messageDedup struct {
	entries map[string]time.Time
	ttl     time.Duration
	mu      sync.Mutex
}

// newMessageDedup creates a new message deduplication cache
func newMessageDedup(ttl time.Duration) *messageDedup {
	return &messageDedup{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// Seen checks if a message key has been seen recently
func (d *messageDedup) Seen(key string) bool {
	d.mu.Lock()
	_, ok := d.entries[key]
	d.mu.Unlock()
	return ok
}

// Add records a message key with the current timestamp
func (d *messageDedup) Add(key string) {
	d.mu.Lock()
	d.entries[key] = time.Now()
	d.mu.Unlock()
}

// Cleanup removes expired entries
func (d *messageDedup) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := time.Now().Add(-d.ttl)
	for k, v := range d.entries {
		if v.Before(cutoff) {
			delete(d.entries, k)
		}
	}
}

// messageKey computes a unique key for a message based on its content hash
func messageKey(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// NewBridge creates a new bridge instance
func NewBridge(cfg BridgeConfig, logger *slog.Logger) (*Bridge, error) {
	// Set defaults
	if cfg.GroupAddr == "" {
		cfg.GroupAddr = protocol.DefaultMulticastAddr
	}
	if cfg.Port == 0 {
		cfg.Port = protocol.DefaultPort
	}
	if cfg.OfflineTimeout == 0 {
		cfg.OfflineTimeout = 5 * time.Minute
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = 1 * time.Minute
	}
	if cfg.ProxyPort == 0 {
		cfg.ProxyPort = protocol.DefaultPort
	}

	bridge := &Bridge{
		senders:     make(map[string]*multicast.Sender),
		logger:      logger.With("component", "bridge"),
		deviceAlias: cfg.DeviceAlias,
		fingerprint: cfg.Fingerprint,
		excludedFP:  cfg.ExcludeFP,
		dedup:       newMessageDedup(10 * time.Second), // 10s TTL prevents loops while allowing normal announcements
	}

	// Create device tracker
	bridge.tracker = NewDeviceTracker(cfg.OfflineTimeout, logger)

	// Create listeners and senders for each interface
	for _, ifaceName := range cfg.Interfaces {
		// Create listener
		listener, err := multicast.NewListener(ifaceName, cfg.GroupAddr, cfg.Port, logger)
		if err != nil {
			// Log but continue with other interfaces
			logger.Warn("failed to create listener for interface, skipping",
				"iface", ifaceName,
				"error", err,
			)
			continue
		}
		bridge.listeners = append(bridge.listeners, listener)

		// Create sender
		sender, err := multicast.NewSender(ifaceName, cfg.GroupAddr, cfg.Port, logger)
		if err != nil {
			logger.Warn("failed to create sender for interface, skipping",
				"iface", ifaceName,
				"error", err,
			)
			continue
		}
		bridge.senders[ifaceName] = sender
	}

	if len(bridge.listeners) == 0 {
		return nil, fmt.Errorf("no valid interfaces to listen on")
	}

	// Create proxy if enabled
	if cfg.ProxyEnabled {
		proxy, err := NewProxy(cfg.ProxyPort, bridge.tracker, logger)
		if err != nil {
			logger.Warn("failed to create proxy, continuing without it",
				"error", err,
			)
		} else {
			bridge.proxy = proxy
		}
	}

	return bridge, nil
}

// Run starts the bridge and all its components
func (b *Bridge) Run(ctx context.Context) error {
	ctx, b.cancel = context.WithCancel(ctx)

	// Start all listeners
	for _, listener := range b.listeners {
		msgCh, err := listener.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start listener: %w", err)
		}
		b.msgChans = append(b.msgChans, msgCh)
	}

	// Start device tracker cleanup loop
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.tracker.CleanupLoop(1*time.Minute, ctx.Done())
	}()

	// Start dedup cleanup loop
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.dedup.Cleanup()
			}
		}
	}()

	// Start proxy if enabled
	if b.proxy != nil {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			if err := b.proxy.Start(ctx); err != nil {
				b.logger.Error("proxy error", "error", err)
			}
		}()
	}

	// Set up device callbacks
	b.tracker.SetOnJoin(func(device *protocol.DeviceInfo) {
		b.logger.Info("device discovered",
			"alias", device.Alias,
			"ip", device.IP.String(),
			"iface", device.SourceIface,
			"type", device.DeviceType,
			"model", device.DeviceModel,
		)
	})

	b.tracker.SetOnLeave(func(device *protocol.DeviceInfo) {
		b.logger.Info("device offline",
			"alias", device.Alias,
			"ip", device.IP.String(),
			"iface", device.SourceIface,
		)
	})

	// Start the bridge multiplexer
	b.wg.Add(1)
	go b.multiplexer(ctx)

	b.logger.Info("bridge started",
		"interfaces", len(b.listeners),
		"devices_tracked", b.tracker.Count(),
		"dedup_ttl", b.dedup.ttl.String(),
	)

	return nil
}

// multiplexer receives messages from all listeners and processes them
func (b *Bridge) multiplexer(ctx context.Context) {
	defer b.wg.Done()

	var wg sync.WaitGroup
	for _, ch := range b.msgChans {
		wg.Add(1)
		go func(c chan *multicast.Message) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-c:
					if !ok {
						return
					}
					b.processMessage(msg)
				}
			}
		}(ch)
	}
	wg.Wait()
}

// processMessage handles a single multicast message
func (b *Bridge) processMessage(msg *multicast.Message) {
	// Check if this is a LocalSend discovery message
	if !protocol.IsDiscoveryMessage(msg.Data) {
		return
	}

	// Deduplication: skip messages we've already processed recently
	// This prevents forwarding loops caused by bridged interfaces (e.g., br10 + vxlan10)
	// where a forwarded message on one interface may be received on another.
	key := messageKey(msg.Data)
	if b.dedup.Seen(key) {
		b.logger.Debug("skipping duplicate message",
			"from", msg.From.String(),
			"iface", msg.Iface,
		)
		return
	}
	b.dedup.Add(key)

	// Parse the message
	discoveryMsg, err := protocol.ParseDiscoveryMessage(msg.Data, msg.From)
	if err != nil {
		b.logger.Debug("failed to parse discovery message",
			"from", msg.From.String(),
			"error", err,
		)
		return
	}

	// Skip self-discovery
	if b.isSelf(discoveryMsg.Fingerprint) {
		return
	}

	// Skip if fingerprint is excluded
	if b.isExcluded(discoveryMsg.Fingerprint) {
		return
	}

	// Skip if the message is an announce=false response (already handled)
	if discoveryMsg.Announce != nil && !*discoveryMsg.Announce {
		// This is a response to a discovery, but we still track the device
		b.tracker.AddOrUpdate(discoveryMsg, msg.From, msg.Iface, msg.IfaceIP)
		return
	}

	// Record the device
	device := b.tracker.AddOrUpdate(discoveryMsg, msg.From, msg.Iface, msg.IfaceIP)

	// Forward the message to all other interfaces
	b.forwardToOtherInterfaces(msg, device)
}

// forwardToOtherInterfaces forwards the discovery message to all interfaces except the source
func (b *Bridge) forwardToOtherInterfaces(msg *multicast.Message, device *protocol.DeviceInfo) {
	for ifaceName, sender := range b.senders {
		// Skip the source interface
		if ifaceName == msg.Iface {
			continue
		}

		// Send the original multicast data to the other interface's multicast group
		if err := sender.Send(msg.Data); err != nil {
			b.logger.Warn("failed to forward message to interface",
				"iface", ifaceName,
				"from", msg.From.String(),
				"error", err,
			)
			continue
		}

		b.logger.Debug("forwarded discovery message",
			"alias", device.Alias,
			"from_iface", msg.Iface,
			"to_iface", ifaceName,
			"from_ip", msg.From.IP.String(),
		)
	}
}

// isSelf checks if the fingerprint belongs to this bridge
func (b *Bridge) isSelf(fingerprint string) bool {
	return b.fingerprint != "" && fingerprint == b.fingerprint
}

// isExcluded checks if the fingerprint is in the exclusion list
func (b *Bridge) isExcluded(fingerprint string) bool {
	for _, fp := range b.excludedFP {
		if fp == fingerprint {
			return true
		}
	}
	return false
}

// GetTracker returns the device tracker
func (b *Bridge) GetTracker() *DeviceTracker {
	return b.tracker
}

// Stop gracefully stops the bridge
func (b *Bridge) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.wg.Wait()

	// Close all listeners and senders
	for _, listener := range b.listeners {
		listener.Close()
	}
	for _, sender := range b.senders {
		sender.Close()
	}

	if b.proxy != nil {
		b.proxy.Stop()
	}

	b.logger.Info("bridge stopped")
}
