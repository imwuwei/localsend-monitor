package multicast

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
)

// Sender sends UDP multicast messages on a specific network interface
type Sender struct {
	iface     *net.Interface
	groupAddr *net.UDPAddr
	raw       *ipv4.RawConn
	conn      net.PacketConn
	logger    *slog.Logger
	hasRaw    bool
}

// NewSender creates a new multicast sender for the specified interface.
// It attempts to create a raw socket for source IP preservation,
// and falls back to a regular UDP socket if raw sockets are not available.
func NewSender(ifaceName string, groupAddr string, port int, logger *slog.Logger) (*Sender, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", groupAddr, port))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	s := &Sender{
		iface:     iface,
		groupAddr: udpAddr,
		logger:    logger.With("iface", ifaceName, "role", "sender"),
	}

	// Try to create a raw socket for source IP preservation
	// This requires CAP_NET_RAW or root privileges
	if err := s.initRawSocket(); err != nil {
		s.logger.Warn("raw socket initialization failed, falling back to regular UDP socket",
			"error", err,
		)
		// Fall back to regular UDP socket
		conn, err := net.ListenPacket("udp", ":0")
		if err != nil {
			return nil, fmt.Errorf("failed to create UDP socket: %w", err)
		}
		s.conn = conn
	}

	return s, nil
}

// initRawSocket creates a raw IP socket and binds it to the interface.
// This is needed for source IP preservation (SendFrom).
func (s *Sender) initRawSocket() error {
	// Create a raw IP socket for UDP protocol
	// This socket will be used to send packets with custom source IPs
	conn, err := net.ListenPacket("ip4:udp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("failed to create raw socket: %w", err)
	}

	// Bind the socket to the specific interface using SO_BINDTODEVICE
	// This ensures the packet goes out through the correct interface
	ipConn := conn.(*net.IPConn)
	rawConn, err := ipConn.SyscallConn()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to get syscall conn: %w", err)
	}

	var bindErr error
	rawConn.Control(func(fd uintptr) {
		bindErr = syscall.SetsockoptString(
			int(fd),
			syscall.SOL_SOCKET,
			syscall.SO_BINDTODEVICE,
			s.iface.Name,
		)
	})
	if bindErr != nil {
		conn.Close()
		return fmt.Errorf("failed to bind raw socket to interface %s: %w", s.iface.Name, bindErr)
	}

	// Wrap with ipv4.RawConn for convenient IP header construction
	raw, err := ipv4.NewRawConn(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create raw conn: %w", err)
	}

	s.raw = raw
	s.conn = conn
	s.hasRaw = true
	return nil
}

// Send sends data to the multicast group via the associated interface.
// Uses the regular UDP socket. The kernel will choose the source IP
// based on the routing table for the outbound interface.
func (s *Sender) Send(data []byte) error {
	if s.conn == nil {
		return fmt.Errorf("sender is not initialized")
	}

	s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	n, err := s.conn.WriteTo(data, s.groupAddr)
	if err != nil {
		return fmt.Errorf("failed to send multicast message: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf("partial write: %d of %d bytes", n, len(data))
	}

	return nil
}

// SendFrom sends data to the multicast group while preserving the original
// sender's source IP address. This is critical for LocalSend device discovery,
// which relies on the UDP packet's source IP to identify devices.
//
// Uses a raw socket with IP_HDRINCL to construct the full IP+UDP header
// manually, allowing us to set an arbitrary source IP.
//
// Falls back to regular Send() if raw sockets are not available.
func (s *Sender) SendFrom(data []byte, srcIP net.IP, srcPort int) error {
	if !s.hasRaw || s.raw == nil {
		s.logger.Debug("raw socket not available, falling back to regular send",
			"src_ip", srcIP.String(),
		)
		return s.Send(data)
	}

	// Build UDP header (8 bytes)
	udpLen := 8 + len(data)
	udpHeader := make([]byte, 8)
	binary.BigEndian.PutUint16(udpHeader[0:2], uint16(srcPort))          // Source port (original sender's port)
	binary.BigEndian.PutUint16(udpHeader[2:4], uint16(s.groupAddr.Port)) // Destination port (multicast port)
	binary.BigEndian.PutUint16(udpHeader[4:6], uint16(udpLen))           // UDP length
	binary.BigEndian.PutUint16(udpHeader[6:8], 0)                        // UDP checksum (0 = optional for IPv4)

	// Combine UDP header + payload
	payload := append(udpHeader, data...)

	// Build IP header
	// With IP_HDRINCL, the kernel will use this header as-is,
	// including the source IP. The kernel will compute the IP checksum
	// if we set it to 0.
	header := &ipv4.Header{
		Version:  4,
		Len:      20, // 20 bytes standard header (no options)
		TOS:      0,
		TotalLen: 20 + udpLen, // IP header + UDP header + data
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      64,
		Protocol: 17, // IPPROTO_UDP
		Checksum: 0,  // 0 = kernel computes it
		Src:      srcIP,
		Dst:      s.groupAddr.IP,
	}

	s.raw.SetWriteDeadline(time.Now().Add(5 * time.Second))

	if err := s.raw.WriteTo(header, payload, nil); err != nil {
		// If raw write fails, fall back to regular send
		s.logger.Warn("raw socket send failed, falling back to regular send",
			"error", err,
			"src_ip", srcIP.String(),
		)
		return s.Send(data)
	}

	s.logger.Debug("forwarded multicast with original source IP preserved via raw socket",
		"src_ip", srcIP.String(),
		"src_port", srcPort,
		"iface", s.iface.Name,
		"ifindex", s.iface.Index,
		"dst", s.groupAddr.String(),
		"data_len", len(data),
	)

	return nil
}

// SendTo sends data to a specific UDP address via the associated interface
func (s *Sender) SendTo(data []byte, addr *net.UDPAddr) error {
	if s.conn == nil {
		return fmt.Errorf("sender is not initialized")
	}

	s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	n, err := s.conn.WriteTo(data, addr)
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
