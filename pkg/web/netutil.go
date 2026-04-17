package web

import (
	"fmt"
	"net"
)

// detectLANIP returns the first non-loopback IPv4 address found on the host.
// Prefers private (RFC1918) addresses. Returns an error if none found.
func detectLANIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("list interfaces: %w", err)
	}

	var fallback string

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}

		ip := ipNet.IP.To4()

		// Prefer RFC1918 private ranges
		if isPrivateIP(ip) {
			return ip.String(), nil
		}

		if fallback == "" {
			fallback = ip.String()
		}
	}

	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("no suitable LAN IPv4 address found")
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []struct {
		start net.IP
		mask  net.IPMask
	}{
		{net.IP{10, 0, 0, 0}, net.CIDRMask(8, 32)},
		{net.IP{172, 16, 0, 0}, net.CIDRMask(12, 32)},
		{net.IP{192, 168, 0, 0}, net.CIDRMask(16, 32)},
	}
	for _, r := range privateRanges {
		network := &net.IPNet{IP: r.start, Mask: r.mask}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
