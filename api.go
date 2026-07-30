package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/localsend-monitor/src/protocol"
	"github.com/localsend-monitor/src/relay"
)

// APIServer provides HTTP API for monitoring and managing the bridge
type APIServer struct {
	bridge  *relay.Bridge
	server  *http.Server
	logger  *slog.Logger
	started time.Time
}

// NewAPIServer creates a new API server
func NewAPIServer(bridge *relay.Bridge, listenAddr string, port int, logger *slog.Logger) *APIServer {
	api := &APIServer{
		bridge:  bridge,
		logger:  logger.With("component", "api"),
		started: time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/devices", api.handleDevices)
	mux.HandleFunc("/api/devices/", api.handleDeviceByKey)
	mux.HandleFunc("/api/stats", api.handleStats)
	mux.HandleFunc("/api/health", api.handleHealth)
	mux.HandleFunc("/api/interfaces", api.handleInterfaces)

	api.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", listenAddr, port),
		Handler: mux,
	}

	return api
}

// Start starts the API server
func (s *APIServer) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("API server starting",
			"addr", s.server.Addr,
		)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return s.server.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Stop stops the API server
func (s *APIServer) Stop() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}
}

// handleDevices returns all discovered devices
func (s *APIServer) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	devices := s.bridge.GetTracker().GetAll()
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Alias < devices[j].Alias
	})

	response := struct {
		Count   int                    `json:"count"`
		Devices []*protocol.DeviceInfo `json:"devices"`
	}{
		Count:   len(devices),
		Devices: devices,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDeviceByKey returns a specific device by fingerprint or IP:port
func (s *APIServer) handleDeviceByKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract key from path: /api/devices/{key}
	key := r.URL.Path[len("/api/devices/"):]
	if key == "" {
		http.Error(w, "Device key required", http.StatusBadRequest)
		return
	}

	device := s.bridge.GetTracker().Get(key)
	if device == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device)
}

// handleStats returns bridge statistics
func (s *APIServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	devices := s.bridge.GetTracker().GetAll()
	onlineCount := 0
	ifaceCounts := make(map[string]int)
	typeCounts := make(map[string]int)

	for _, d := range devices {
		if d.Online {
			onlineCount++
		}
		ifaceCounts[d.SourceIface]++
		typeCounts[string(d.DeviceType)]++
	}

	stats := struct {
		TotalDevices   int            `json:"totalDevices"`
		OnlineDevices  int            `json:"onlineDevices"`
		OfflineDevices int            `json:"offlineDevices"`
		ByInterface    map[string]int `json:"byInterface"`
		ByDeviceType   map[string]int `json:"byDeviceType"`
		Uptime         string         `json:"uptime"`
		Version        string         `json:"version"`
	}{
		TotalDevices:   len(devices),
		OnlineDevices:  onlineCount,
		OfflineDevices: len(devices) - onlineCount,
		ByInterface:    ifaceCounts,
		ByDeviceType:   typeCounts,
		Uptime:         time.Since(s.started).Round(time.Second).String(),
		Version:        Version,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleHealth returns health check information
func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"uptime":  time.Since(s.started).Round(time.Second).String(),
		"version": Version,
		"devices": s.bridge.GetTracker().Count(),
	})
}

// handleInterfaces returns network interface information
func (s *APIServer) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	// Get interfaces from the bridge config
	interfaces := s.bridge.GetInterfaces()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"interfaces": interfaces,
	})
}
