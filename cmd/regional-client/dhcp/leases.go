package dhcp

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// LeaseManager manages DHCP leases
type LeaseManager struct {
	startIP   net.IP
	endIP     net.IP
	leaseTime time.Duration

	leases    map[string]*Lease  // IP -> Lease
	macToIP   map[string]net.IP  // MAC -> IP
	mu        sync.RWMutex
}

// Lease represents a DHCP lease
type Lease struct {
	MAC        net.HardwareAddr
	IP         net.IP
	Hostname   string
	ExpireTime time.Time
	CreatedAt  time.Time
}

// NewLeaseManager creates a new lease manager
func NewLeaseManager(startIP, endIP net.IP, leaseTime time.Duration) *LeaseManager {
	lm := &LeaseManager{
		startIP:   startIP.To4(),
		endIP:     endIP.To4(),
		leaseTime: leaseTime,
		leases:    make(map[string]*Lease),
		macToIP:   make(map[string]net.IP),
	}

	// Start cleanup goroutine
	go lm.cleanupExpired()

	return lm
}

// Allocate allocates an IP for a MAC address
func (lm *LeaseManager) Allocate(mac net.HardwareAddr) (net.IP, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Check if MAC already has a lease
	if existingIP, ok := lm.macToIP[mac.String()]; ok {
		// Renew existing lease
		if lease, exists := lm.leases[existingIP.String()]; exists {
			lease.ExpireTime = time.Now().Add(lm.leaseTime)
			return existingIP, nil
		}
	}

	// Find available IP
	ip := lm.findAvailableIP()
	if ip == nil {
		return nil, fmt.Errorf("no available IP addresses in pool")
	}

	// Create lease
	lease := &Lease{
		MAC:        mac,
		IP:         ip,
		ExpireTime: time.Now().Add(lm.leaseTime),
		CreatedAt:  time.Now(),
	}

	lm.leases[ip.String()] = lease
	lm.macToIP[mac.String()] = ip

	return ip, nil
}

// Release releases a lease
func (lm *LeaseManager) Release(mac net.HardwareAddr, ip net.IP) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	delete(lm.leases, ip.String())
	delete(lm.macToIP, mac.String())
}

// IsAllocated checks if a MAC has the given IP allocated
func (lm *LeaseManager) IsAllocated(mac net.HardwareAddr, ip net.IP) bool {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	allocatedIP, ok := lm.macToIP[mac.String()]
	if !ok {
		return false
	}

	return allocatedIP.Equal(ip)
}

// GetLease gets a lease by IP
func (lm *LeaseManager) GetLease(ip net.IP) (*Lease, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	lease, ok := lm.leases[ip.String()]
	return lease, ok
}

// GetAll returns all current leases
func (lm *LeaseManager) GetAll() []*Lease {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	leases := make([]*Lease, 0, len(lm.leases))
	for _, lease := range lm.leases {
		leases = append(leases, lease)
	}
	return leases
}

// findAvailableIP finds an available IP in the pool
func (lm *LeaseManager) findAvailableIP() net.IP {
	// Iterate through IP range
	for ip := copyIP(lm.startIP); !ipGreaterThan(ip, lm.endIP); incIP(ip) {
		if _, used := lm.leases[ip.String()]; !used {
			return copyIP(ip)
		}
	}
	return nil
}

// cleanupExpired periodically removes expired leases
func (lm *LeaseManager) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		lm.mu.Lock()
		now := time.Now()

		for ipStr, lease := range lm.leases {
			if now.After(lease.ExpireTime) {
				delete(lm.leases, ipStr)
				delete(lm.macToIP, lease.MAC.String())
			}
		}

		lm.mu.Unlock()
	}
}

// copyIP creates a copy of an IP address
func copyIP(ip net.IP) net.IP {
	dup := make(net.IP, len(ip))
	copy(dup, ip)
	return dup
}

// incIP increments an IP address
func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// ipGreaterThan checks if ip1 > ip2
func ipGreaterThan(ip1, ip2 net.IP) bool {
	ip1 = ip1.To4()
	ip2 = ip2.To4()

	for i := 0; i < 4; i++ {
		if ip1[i] > ip2[i] {
			return true
		}
		if ip1[i] < ip2[i] {
			return false
		}
	}
	return false
}
