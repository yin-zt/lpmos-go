package dhcp

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/krolaw/dhcp4"
)

// Server represents a DHCP server
type Server struct {
	Interface    string
	ServerIP     net.IP
	Gateway      net.IP
	DNSServers   []net.IP
	TFTPServer   net.IP
	BootFile     string
	LeaseTime    time.Duration

	// IP Pool
	StartIP      net.IP
	EndIP        net.IP
	Netmask      net.IP

	// Lease management
	leases       *LeaseManager
	staticBinds  map[string]*StaticBinding  // MAC -> Binding

	conn         *net.UDPConn
	stopChan     chan struct{}
	mu           sync.RWMutex
}

// StaticBinding represents a static MAC-IP binding
type StaticBinding struct {
	MAC        net.HardwareAddr
	IP         net.IP
	Hostname   string
	BootFile   string  // Custom boot file for this MAC
}

// Config holds DHCP server configuration
type Config struct {
	Interface    string
	ServerIP     string
	Gateway      string
	DNSServers   []string
	TFTPServer   string
	BootFile     string
	LeaseTime    time.Duration
	StartIP      string
	EndIP        string
	Netmask      string
}

// NewServer creates a new DHCP server
func NewServer(config Config) (*Server, error) {
	// Parse IPs
	serverIP := net.ParseIP(config.ServerIP)
	if serverIP == nil {
		return nil, fmt.Errorf("invalid server IP: %s", config.ServerIP)
	}

	gateway := net.ParseIP(config.Gateway)
	if gateway == nil {
		return nil, fmt.Errorf("invalid gateway: %s", config.Gateway)
	}

	tftpServer := net.ParseIP(config.TFTPServer)
	if tftpServer == nil {
		return nil, fmt.Errorf("invalid TFTP server: %s", config.TFTPServer)
	}

	startIP := net.ParseIP(config.StartIP)
	if startIP == nil {
		return nil, fmt.Errorf("invalid start IP: %s", config.StartIP)
	}

	endIP := net.ParseIP(config.EndIP)
	if endIP == nil {
		return nil, fmt.Errorf("invalid end IP: %s", config.EndIP)
	}

	netmask := net.ParseIP(config.Netmask)
	if netmask == nil {
		return nil, fmt.Errorf("invalid netmask: %s", config.Netmask)
	}

	// Parse DNS servers
	var dnsServers []net.IP
	for _, dns := range config.DNSServers {
		dnsIP := net.ParseIP(dns)
		if dnsIP == nil {
			return nil, fmt.Errorf("invalid DNS server: %s", dns)
		}
		dnsServers = append(dnsServers, dnsIP)
	}

	server := &Server{
		Interface:   config.Interface,
		ServerIP:    serverIP.To4(),
		Gateway:     gateway.To4(),
		DNSServers:  dnsServers,
		TFTPServer:  tftpServer.To4(),
		BootFile:    config.BootFile,
		LeaseTime:   config.LeaseTime,
		StartIP:     startIP.To4(),
		EndIP:       endIP.To4(),
		Netmask:     netmask.To4(),
		leases:      NewLeaseManager(startIP, endIP, config.LeaseTime),
		staticBinds: make(map[string]*StaticBinding),
		stopChan:    make(chan struct{}),
	}

	return server, nil
}

// Start starts the DHCP server
func (s *Server) Start() error {
	// Listen on UDP port 67
	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: 67,
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.conn = conn

	log.Printf("[DHCP] Server started on %s:%d", s.Interface, 67)
	log.Printf("[DHCP] IP Pool: %s - %s", s.StartIP, s.EndIP)
	log.Printf("[DHCP] Gateway: %s, TFTP: %s", s.Gateway, s.TFTPServer)

	// Start serving
	go s.serve()

	return nil
}

// Stop stops the DHCP server
func (s *Server) Stop() error {
	close(s.stopChan)
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// serve handles incoming DHCP packets
func (s *Server) serve() {
	buffer := make([]byte, 1500)

	for {
		select {
		case <-s.stopChan:
			return
		default:
			s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, addr, err := s.conn.ReadFrom(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Printf("[DHCP] Error reading: %v", err)
				continue
			}

			// Parse DHCP packet
			packet := dhcp4.Packet(buffer[:n])
			if err := s.handlePacket(packet, addr); err != nil {
				log.Printf("[DHCP] Error handling packet: %v", err)
			}
		}
	}
}

// handlePacket processes a DHCP packet
func (s *Server) handlePacket(packet dhcp4.Packet, addr net.Addr) error {
	msgType := packet.ParseOptions()[dhcp4.OptionDHCPMessageType]
	if len(msgType) == 0 {
		return fmt.Errorf("no message type")
	}

	mac := packet.CHAddr()

	switch dhcp4.MessageType(msgType[0]) {
	case dhcp4.Discover:
		return s.handleDiscover(packet, mac)
	case dhcp4.Request:
		return s.handleRequest(packet, mac)
	case dhcp4.Release:
		return s.handleRelease(packet, mac)
	case dhcp4.Decline:
		return s.handleDecline(packet, mac)
	}

	return nil
}

// handleDiscover handles DHCP Discover
func (s *Server) handleDiscover(packet dhcp4.Packet, mac net.HardwareAddr) error {
	log.Printf("[DHCP] DISCOVER from %s", mac)

	// Check static binding first
	var offeredIP net.IP
	var bootFile string

	s.mu.RLock()
	if binding, ok := s.staticBinds[mac.String()]; ok {
		offeredIP = binding.IP
		if binding.BootFile != "" {
			bootFile = binding.BootFile
		} else {
			bootFile = s.BootFile
		}
		log.Printf("[DHCP] Static binding found: %s -> %s", mac, offeredIP)
	}
	s.mu.RUnlock()

	// If no static binding, allocate from pool
	if offeredIP == nil {
		var err error
		offeredIP, err = s.leases.Allocate(mac)
		if err != nil {
			log.Printf("[DHCP] Failed to allocate IP: %v", err)
			return err
		}
		bootFile = s.BootFile
		log.Printf("[DHCP] Allocated IP from pool: %s -> %s", mac, offeredIP)
	}

	// Build DHCP Offer
	options := dhcp4.Options{
		dhcp4.OptionDHCPMessageType: []byte{byte(dhcp4.Offer)},
		dhcp4.OptionServerIdentifier: []byte(s.ServerIP),
		dhcp4.OptionRouter:           []byte(s.Gateway),
		dhcp4.OptionSubnetMask:       []byte(s.Netmask),
		dhcp4.OptionDomainNameServer: s.joinIPs(s.DNSServers),
		dhcp4.OptionIPAddressLeaseTime: dhcp4.OptionsLeaseTime(s.LeaseTime),
	}

	reply := dhcp4.ReplyPacket(packet, dhcp4.Offer, s.ServerIP, offeredIP, s.LeaseTime, options.SelectOrderOrAll(packet.ParseOptions()[dhcp4.OptionParameterRequestList]))

	// Add PXE options
	reply.AddOption(dhcp4.OptionTFTPServerName, []byte(s.TFTPServer.String()))
	reply.AddOption(dhcp4.OptionBootFileName, []byte(bootFile))

	// Send reply
	return s.sendPacket(reply)
}

// handleRequest handles DHCP Request
func (s *Server) handleRequest(packet dhcp4.Packet, mac net.HardwareAddr) error {
	requestedIP := packet.CIAddr()
	if requestedIP.Equal(net.IPv4zero) {
		// Extract requested IP from options
		if reqIP := packet.ParseOptions()[dhcp4.OptionRequestedIPAddress]; len(reqIP) == 4 {
			requestedIP = net.IP(reqIP)
		}
	}

	log.Printf("[DHCP] REQUEST from %s for %s", mac, requestedIP)

	// Verify the request
	var assignedIP net.IP
	var bootFile string

	s.mu.RLock()
	if binding, ok := s.staticBinds[mac.String()]; ok {
		if binding.IP.Equal(requestedIP) {
			assignedIP = binding.IP
			if binding.BootFile != "" {
				bootFile = binding.BootFile
			} else {
				bootFile = s.BootFile
			}
		}
	}
	s.mu.RUnlock()

	if assignedIP == nil {
		// Check lease
		if s.leases.IsAllocated(mac, requestedIP) {
			assignedIP = requestedIP
			bootFile = s.BootFile
		}
	}

	if assignedIP == nil {
		// NAK
		log.Printf("[DHCP] NAK to %s (invalid request)", mac)
		options := dhcp4.Options{
			dhcp4.OptionDHCPMessageType: []byte{byte(dhcp4.NAK)},
			dhcp4.OptionServerIdentifier: []byte(s.ServerIP),
		}
		reply := dhcp4.ReplyPacket(packet, dhcp4.NAK, s.ServerIP, nil, 0, options.SelectOrderOrAll(nil))
		return s.sendPacket(reply)
	}

	// ACK
	log.Printf("[DHCP] ACK to %s: %s", mac, assignedIP)
	options := dhcp4.Options{
		dhcp4.OptionDHCPMessageType: []byte{byte(dhcp4.ACK)},
		dhcp4.OptionServerIdentifier: []byte(s.ServerIP),
		dhcp4.OptionRouter:           []byte(s.Gateway),
		dhcp4.OptionSubnetMask:       []byte(s.Netmask),
		dhcp4.OptionDomainNameServer: s.joinIPs(s.DNSServers),
		dhcp4.OptionIPAddressLeaseTime: dhcp4.OptionsLeaseTime(s.LeaseTime),
	}

	reply := dhcp4.ReplyPacket(packet, dhcp4.ACK, s.ServerIP, assignedIP, s.LeaseTime, options.SelectOrderOrAll(packet.ParseOptions()[dhcp4.OptionParameterRequestList]))

	// Add PXE options
	reply.AddOption(dhcp4.OptionTFTPServerName, []byte(s.TFTPServer.String()))
	reply.AddOption(dhcp4.OptionBootFileName, []byte(bootFile))

	return s.sendPacket(reply)
}

// handleRelease handles DHCP Release
func (s *Server) handleRelease(packet dhcp4.Packet, mac net.HardwareAddr) error {
	ip := packet.CIAddr()
	log.Printf("[DHCP] RELEASE from %s: %s", mac, ip)
	s.leases.Release(mac, ip)
	return nil
}

// handleDecline handles DHCP Decline
func (s *Server) handleDecline(packet dhcp4.Packet, mac net.HardwareAddr) error {
	if reqIP := packet.ParseOptions()[dhcp4.OptionRequestedIPAddress]; len(reqIP) == 4 {
		ip := net.IP(reqIP)
		log.Printf("[DHCP] DECLINE from %s: %s", mac, ip)
		s.leases.Release(mac, ip)
	}
	return nil
}

// sendPacket sends a DHCP packet
func (s *Server) sendPacket(packet dhcp4.Packet) error {
	// Broadcast to 255.255.255.255:68
	addr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: 68,
	}

	_, err := s.conn.WriteTo(packet, addr)
	return err
}

// AddStaticBinding adds a static MAC-IP binding
func (s *Server) AddStaticBinding(mac string, ip string, hostname string, bootFile string) error {
	hwAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("invalid MAC address: %w", err)
	}

	ipAddr := net.ParseIP(ip)
	if ipAddr == nil {
		return fmt.Errorf("invalid IP address: %s", ip)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.staticBinds[mac] = &StaticBinding{
		MAC:      hwAddr,
		IP:       ipAddr.To4(),
		Hostname: hostname,
		BootFile: bootFile,
	}

	log.Printf("[DHCP] Added static binding: %s -> %s (boot: %s)", mac, ip, bootFile)
	return nil
}

// RemoveStaticBinding removes a static binding
func (s *Server) RemoveStaticBinding(mac string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.staticBinds, mac)
	log.Printf("[DHCP] Removed static binding: %s", mac)
	return nil
}

// GetLeases returns all current leases
func (s *Server) GetLeases() []*Lease {
	return s.leases.GetAll()
}

// GetStaticBindings returns all static bindings
func (s *Server) GetStaticBindings() map[string]*StaticBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy
	bindings := make(map[string]*StaticBinding)
	for k, v := range s.staticBinds {
		bindings[k] = v
	}
	return bindings
}

// joinIPs joins multiple IPs into a byte slice
func (s *Server) joinIPs(ips []net.IP) []byte {
	result := make([]byte, 0, len(ips)*4)
	for _, ip := range ips {
		result = append(result, ip.To4()...)
	}
	return result
}
