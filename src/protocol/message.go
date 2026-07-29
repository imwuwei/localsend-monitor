package protocol

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// Constants for LocalSend protocol
const (
	DefaultMulticastAddr = "224.0.0.167"
	DefaultPort          = 53317
	ProtocolVersion      = "2.0"
)

// DeviceType represents the type of device
type DeviceType string

const (
	DeviceTypeMobile   DeviceType = "mobile"
	DeviceTypeDesktop  DeviceType = "desktop"
	DeviceTypeWeb      DeviceType = "web"
	DeviceTypeHeadless DeviceType = "headless"
	DeviceTypeServer   DeviceType = "server"
)

// DiscoveryMessage represents a UDP multicast announcement
type DiscoveryMessage struct {
	Alias       string     `json:"alias"`
	Version     string     `json:"version"`
	DeviceModel string     `json:"deviceModel,omitempty"`
	DeviceType  DeviceType `json:"deviceType,omitempty"`
	Fingerprint string     `json:"fingerprint"`
	Port        int        `json:"port"`
	Protocol    string     `json:"protocol"` // "http" or "https"
	Download    bool       `json:"download,omitempty"`
	Announce    *bool      `json:"announce,omitempty"`
}

// RegisterMessage is the HTTP POST /api/localsend/v2/register request body
type RegisterMessage struct {
	Alias       string     `json:"alias"`
	Version     string     `json:"version"`
	DeviceModel string     `json:"deviceModel,omitempty"`
	DeviceType  DeviceType `json:"deviceType,omitempty"`
	Fingerprint string     `json:"fingerprint"`
	Port        int        `json:"port"`
	Protocol    string     `json:"protocol"`
	Download    bool       `json:"download,omitempty"`
}

// DeviceInfo holds complete device information tracked by the bridge
type DeviceInfo struct {
	Alias       string     `json:"alias"`
	Version     string     `json:"version"`
	DeviceModel string     `json:"deviceModel,omitempty"`
	DeviceType  DeviceType `json:"deviceType,omitempty"`
	Fingerprint string     `json:"fingerprint"`
	Port        int        `json:"port"`
	Protocol    string     `json:"protocol"`
	Download    bool       `json:"download,omitempty"`
	IP          net.IP     `json:"ip"`
	SourceMAC   string     `json:"sourceMAC,omitempty"`
	SourceIface string     `json:"sourceIface"`
	LastSeen    int64      `json:"lastSeen"` // Unix timestamp
	Online      bool       `json:"online"`
}

// Key returns a unique identifier for the device
func (d *DeviceInfo) Key() string {
	if d.Fingerprint != "" {
		return d.Fingerprint
	}
	return fmt.Sprintf("%s:%d", d.IP.String(), d.Port)
}

// InterfaceInfo holds information about a network interface
type InterfaceInfo struct {
	Name    string
	IP      net.IP
	Subnet  *net.IPNet
	MACAddr string
}

// ParseDiscoveryMessage parses a UDP multicast discovery message
func ParseDiscoveryMessage(data []byte, fromAddr *net.UDPAddr) (*DiscoveryMessage, error) {
	var msg DiscoveryMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse discovery message: %w", err)
	}

	// Validate required fields
	if msg.Alias == "" {
		return nil, fmt.Errorf("missing required field: alias")
	}
	if msg.Fingerprint == "" {
		return nil, fmt.Errorf("missing required field: fingerprint")
	}

	// Set defaults
	if msg.Version == "" {
		msg.Version = ProtocolVersion
	}
	if msg.Port == 0 {
		msg.Port = DefaultPort
	}
	if msg.Protocol == "" {
		msg.Protocol = "http"
	}

	return &msg, nil
}

// IsDiscoveryMessage checks if the data looks like a discovery message
func IsDiscoveryMessage(data []byte) bool {
	// Discovery messages are JSON objects with "alias" and "fingerprint" fields
	// (or "announce" for v1)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, hasAlias := raw["alias"]
	_, hasAnnounce := raw["announce"]
	_, hasAnnouncement := raw["announcement"]
	return hasAlias || hasAnnounce || hasAnnouncement
}

// IsRegisterRequest checks if the HTTP request body is a register message
func IsRegisterRequest(data []byte) bool {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, hasAlias := raw["alias"]
	_, hasFP := raw["fingerprint"]
	return hasAlias && hasFP
}

// MatchesInterface checks if the device belongs to the specified interface
func (d *DeviceInfo) MatchesInterface(ifaceName string) bool {
	return strings.EqualFold(d.SourceIface, ifaceName)
}
