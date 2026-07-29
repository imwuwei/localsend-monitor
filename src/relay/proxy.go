package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/localsend-monitor/src/protocol"
)

// Proxy handles HTTP-level proxying of LocalSend register requests
// This allows devices on different subnets to initiate file transfers
// by proxying their register requests and discovery responses
type Proxy struct {
	tracker    *DeviceTracker
	port       int
	server     *http.Server
	logger     *slog.Logger
	httpClient *http.Client
	mu         sync.RWMutex
	discoverCh chan *protocol.DiscoveryMessage
}

// NewProxy creates a new HTTP proxy for LocalSend
func NewProxy(port int, tracker *DeviceTracker, logger *slog.Logger) (*Proxy, error) {
	p := &Proxy{
		tracker:    tracker,
		port:       port,
		logger:     logger.With("component", "proxy"),
		discoverCh: make(chan *protocol.DiscoveryMessage, 64),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: 5 * time.Second,
				}).DialContext,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}

	// Create HTTP server with proxy handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/api/localsend/v2/register", p.handleRegister)

	p.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	return p, nil
}

// Start starts the HTTP proxy server
func (p *Proxy) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		p.logger.Info("proxy server starting", "port", p.port)
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return p.server.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Stop gracefully stops the proxy server
func (p *Proxy) Stop() {
	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p.server.Shutdown(ctx)
	}
}

// handleRegister handles incoming register requests from devices
// When a device sends a register request to the proxy, it forwards it
// to all discovered devices on other subnets
func (p *Proxy) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse the register message
	var regMsg protocol.RegisterMessage
	if err := json.Unmarshal(body, &regMsg); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if regMsg.Alias == "" || regMsg.Fingerprint == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Get the source IP from the request
	sourceIP := net.ParseIP(strings.Split(r.RemoteAddr, ":")[0])

	p.logger.Info("received register request",
		"alias", regMsg.Alias,
		"ip", sourceIP.String(),
		"fingerprint", regMsg.Fingerprint[:min(8, len(regMsg.Fingerprint))]+"...",
	)

	// Forward the register request to all known devices on other subnets
	p.forwardRegister(regMsg, sourceIP, body)

	// Respond with a list of known devices on the same subnet
	devices := p.tracker.GetAll()
	response := map[string]interface{}{
		"success": true,
		"devices": devices,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// forwardRegister forwards a register request to all known devices
func (p *Proxy) forwardRegister(regMsg protocol.RegisterMessage, sourceIP net.IP, body []byte) {
	devices := p.tracker.GetAll()
	var wg sync.WaitGroup

	for _, device := range devices {
		// Skip if it's the same IP (the device itself)
		if device.IP.Equal(sourceIP) {
			continue
		}

		// Skip if device is offline
		if !device.Online {
			continue
		}

		wg.Add(1)
		go func(dev *protocol.DeviceInfo) {
			defer wg.Done()
			p.forwardToDevice(dev, body)
		}(device)
	}

	wg.Wait()
}

// forwardToDevice sends a register request to a specific device
func (p *Proxy) forwardToDevice(device *protocol.DeviceInfo, body []byte) {
	// Build the target URL
	scheme := device.Protocol
	if scheme == "" {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://%s:%d%s", scheme, device.IP.String(), device.Port, "/api/localsend/v2/register")

	// Create the request
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		p.logger.Debug("failed to create request",
			"target", device.Alias,
			"error", err,
		)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.Debug("failed to forward register request",
			"target", device.Alias,
			"ip", device.IP.String(),
			"error", err,
		)
		return
	}
	defer resp.Body.Close()

	p.logger.Info("forwarded register request to device",
		"target", device.Alias,
		"ip", device.IP.String(),
		"status", resp.StatusCode,
	)
}

// min returns the smaller of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
