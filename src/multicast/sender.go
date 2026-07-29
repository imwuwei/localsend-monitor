package multicast

import (
	"fmt"
	"log/slog"
	"net"
	"time"
)

// Sender sends UDP multicast messages on a specific network interface
type Sender struct {
	iface     *net.Interface
	groupAddr *net.UDPAddr
	conn      *net.UDPConn
	logger    *slog.Logger
}

// NewSender creates a new multicast sender for the specified interface
func NewSender(ifaceName string, groupAddr string, port int, logger *slog.Logger) (*Sender, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", groupAddr, port))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	// Create a UDP connection for sending
	// We use a nil local address to let the system choose the right source IP
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP connection: %w", err)
	}

	return &Sender{
		iface:     iface,
		groupAddr: udpAddr,
		conn:      conn,
		logger:    logger.With("iface", ifaceName, "role", "sender"),
	}, nil
}

// Send sends data to the multicast group via the associated interface
func (s *Sender) Send(data []byte) error {
	if s.conn == nil {
		return fmt.Errorf("sender is not initialized")
	}

	// Set the source address to the interface's IP to ensure correct routing
	// We need to connect through the specific interface
	// Use the interface's IP as the local address
	s.conn.Close()

	// Get the interface's IP address
	localIP := getInterfaceIP(s.iface)
	if localIP == nil {
		return fmt.Errorf("no IPv4 address found on interface %s", s.iface.Name)
	}

	localAddr := &net.UDPAddr{
		IP:   localIP,
		Port: 0, // Let system choose a port
	}

	var err error
	s.conn, err = net.DialUDP("udp", localAddr, s.groupAddr)
	if err != nil {
		return fmt.Errorf("failed to bind to interface %s: %w", s.iface.Name, err)
	}

	// Set write deadline
	s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	n, err := s.conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send multicast message: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf("partial write: %d of %d bytes", n, len(data))
	}

	return nil
}

// SendTo sends data to a specific UDP address via the associated interface
func (s *Sender) SendTo(data []byte, addr *net.UDPAddr) error {
	if s.conn == nil {
		return fmt.Errorf("sender is not initialized")
	}

	// Use a temporary connection for sending to specific address
	localIP := getInterfaceIP(s.iface)
	if localIP == nil {
		return fmt.Errorf("no IPv4 address found on interface %s", s.iface.Name)
	}

	localAddr := &net.UDPAddr{
		IP:   localIP,
		Port: 0,
	}

	conn, err := net.DialUDP("udp", localAddr, addr)
	if err != nil {
		return fmt.Errorf("failed to create UDP connection to %s: %w", addr.String(), err)
	}
	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	n, err := conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send unicast message: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf("partial write: %d of %d bytes", n, len(data))
	}

	return nil
}

// Close releases the sender's resources
func (s *Sender) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}
