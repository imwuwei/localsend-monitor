package multicast

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"
)

// Message represents a received multicast message with metadata
type Message struct {
	Data     []byte
	From     *net.UDPAddr
	Iface    string
	IfaceIP  net.IP
	Received time.Time
}

// ListenerConfig holds configuration for the listener
type ListenerConfig struct {
	ChannelSize   int
	ReadBufSize   int
	EnableFilter  bool // Enable pre-filtering of non-LocalSend messages
	RateLimit     int  // Max messages per second (0 = unlimited)
	InterfaceName string
	GroupAddr     string
	Port          int
}

// DefaultListenerConfig returns default listener configuration
func DefaultListenerConfig(ifaceName string, groupAddr string, port int) ListenerConfig {
	return ListenerConfig{
		ChannelSize:   4096,
		ReadBufSize:   65535,
		EnableFilter:  true,
		RateLimit:     0,
		InterfaceName: ifaceName,
		GroupAddr:     groupAddr,
		Port:          port,
	}
}

// Listener listens for UDP multicast messages on a specific network interface
type Listener struct {
	iface        *net.Interface
	conn         *net.UDPConn
	groupAddr    *net.UDPAddr
	msgCh        chan *Message
	readBufSize  int
	enableFilter bool
	rateLimit    int
	logger       *slog.Logger
}

// NewListener creates a new multicast listener for the specified interface
func NewListener(ifaceName string, groupAddr string, port int, logger *slog.Logger) (*Listener, error) {
	return NewListenerWithConfig(DefaultListenerConfig(ifaceName, groupAddr, port), logger)
}

// NewListenerWithConfig creates a new multicast listener with custom configuration
func NewListenerWithConfig(cfg ListenerConfig, logger *slog.Logger) (*Listener, error) {
	iface, err := net.InterfaceByName(cfg.InterfaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface %s: %w", cfg.InterfaceName, err)
	}

	// Check if interface is up and has multicast capability
	if iface.Flags&net.FlagUp == 0 {
		return nil, fmt.Errorf("interface %s is down", cfg.InterfaceName)
	}
	if iface.Flags&net.FlagMulticast == 0 {
		return nil, fmt.Errorf("interface %s does not support multicast", cfg.InterfaceName)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", cfg.GroupAddr, cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	// Listen on the wildcard address but bind to the specific interface
	conn, err := net.ListenMulticastUDP("udp", iface, udpAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s:%d (interface: %s): %w",
			cfg.GroupAddr, cfg.Port, cfg.InterfaceName, err)
	}

	// Set large read buffer to avoid kernel drops
	conn.SetReadBuffer(4 * 1024 * 1024) // 4MB buffer

	return &Listener{
		iface:        iface,
		conn:         conn,
		groupAddr:    udpAddr,
		msgCh:        make(chan *Message, cfg.ChannelSize),
		readBufSize:  cfg.ReadBufSize,
		enableFilter: cfg.EnableFilter,
		rateLimit:    cfg.RateLimit,
		logger:       logger.With("iface", cfg.InterfaceName),
	}, nil
}

// Start begins listening for multicast messages in a goroutine
// Returns the message channel for receiving messages
func (l *Listener) Start(ctx context.Context) (chan *Message, error) {
	if l.conn == nil {
		return nil, fmt.Errorf("listener is not initialized")
	}

	go l.readLoop(ctx)

	l.logger.Info("multicast listener started",
		"group", l.groupAddr.String(),
		"iface", l.iface.Name,
		"filter", l.enableFilter,
	)

	return l.msgCh, nil
}

// readLoop continuously reads messages from the multicast group
func (l *Listener) readLoop(ctx context.Context) {
	defer l.conn.Close()
	defer close(l.msgCh)

	buf := make([]byte, l.readBufSize)

	// Rate limiting support
	var lastRateCheck time.Time
	var msgCount int

	for {
		select {
		case <-ctx.Done():
			l.logger.Info("multicast listener stopped")
			return
		default:
			// Set read deadline to allow checking context periodically
			l.conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, from, err := l.conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				select {
				case <-ctx.Done():
					return
				default:
					l.logger.Error("read error", "error", err)
					continue
				}
			}

			if n == 0 {
				continue
			}

			// Pre-filter: quickly reject non-LocalSend messages
			if l.enableFilter && !isPossibleLocalSendMessage(buf[:n]) {
				continue
			}

			// Rate limiting
			if l.rateLimit > 0 {
				now := time.Now()
				if now.Sub(lastRateCheck) >= time.Second {
					lastRateCheck = now
					msgCount = 0
				}
				msgCount++
				if msgCount > l.rateLimit {
					if msgCount == l.rateLimit+1 {
						l.logger.Warn("rate limit exceeded, dropping messages",
							"limit", l.rateLimit,
							"from", from.String(),
						)
					}
					continue
				}
			}

			// Copy data to prevent reuse
			data := make([]byte, n)
			copy(data, buf[:n])

			// Get the interface's IP address
			ifaceIP := getInterfaceIP(l.iface)

			msg := &Message{
				Data:     data,
				From:     from,
				Iface:    l.iface.Name,
				IfaceIP:  ifaceIP,
				Received: time.Now(),
			}

			// Non-blocking send with larger buffer
			select {
			case l.msgCh <- msg:
			default:
				l.logger.Warn("message channel full, dropping message",
					"from", from.String(),
					"chan_size", len(l.msgCh),
				)
			}
		}
	}
}

// isPossibleLocalSendMessage performs a quick pre-filter check
// to determine if the data might be a LocalSend discovery message.
// This avoids expensive JSON parsing for non-LocalSend traffic.
// LocalSend discovery messages are JSON objects containing "alias" or "fingerprint".
func isPossibleLocalSendMessage(data []byte) bool {
	data = bytes.TrimSpace(data)

	// Must start with '{' (JSON object)
	if len(data) < 2 || data[0] != '{' {
		return false
	}

	// Quick substring check for "alias" or "fingerprint" or "announce"
	// These are the key fields that must be present in a LocalSend discovery message
	// Using bytes.Contains is much faster than full JSON parsing
	lower := bytes.ToLower(data)
	return bytes.Contains(lower, []byte(`"alias"`)) ||
		bytes.Contains(lower, []byte(`"fingerprint"`)) ||
		bytes.Contains(lower, []byte(`"announce"`)) ||
		bytes.Contains(lower, []byte(`"announcement"`))
}

// Close stops the listener and releases resources
func (l *Listener) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}

// getInterfaceIP returns the first IPv4 address of the given interface
func getInterfaceIP(iface *net.Interface) net.IP {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if ip := ipnet.IP.To4(); ip != nil {
				return ip
			}
		}
	}
	return nil
}
