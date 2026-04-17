package tun

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	socks5Version    = 0x05
	socks5NoAuth     = 0x00
	socks5CmdConnect = 0x01
	socks5CmdUDP     = 0x03
	socks5AtypIPv4   = 0x01
	socks5Success    = 0x00
)

// DialSOCKS5 establishes a TCP connection through a SOCKS5 proxy.
// proxyAddr: "ip:port" of the SOCKS5 server
// targetIP: destination IPv4
// targetPort: destination port
func DialSOCKS5(proxyAddr string, targetIP net.IP, targetPort uint16) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to proxy %s: %w", proxyAddr, err)
	}

	// Handshake: version + 1 auth method (no auth)
	if _, err := conn.Write([]byte{socks5Version, 1, socks5NoAuth}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 handshake write: %w", err)
	}

	// Read server's method selection
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 handshake read: %w", err)
	}
	if resp[0] != socks5Version || resp[1] != socks5NoAuth {
		conn.Close()
		return nil, fmt.Errorf("socks5 auth rejected: %v", resp)
	}

	// Connect request
	req := make([]byte, 10)
	req[0] = socks5Version
	req[1] = socks5CmdConnect
	req[2] = 0x00 // reserved
	req[3] = socks5AtypIPv4
	copy(req[4:8], targetIP.To4())
	binary.BigEndian.PutUint16(req[8:10], targetPort)

	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect write: %w", err)
	}

	// Read connect response
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect read: %w", err)
	}
	if reply[1] != socks5Success {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect failed: status 0x%02x", reply[1])
	}

	return conn, nil
}

// DialSOCKS5UDP requests a UDP associate from the SOCKS5 proxy.
// Returns the proxy's UDP relay address and the control TCP connection
// (which must stay open for the association to remain valid).
func DialSOCKS5UDP(proxyAddr string) (relayAddr *net.UDPAddr, controlConn net.Conn, err error) {
	conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to proxy %s: %w", proxyAddr, err)
	}

	// Handshake
	if _, err := conn.Write([]byte{socks5Version, 1, socks5NoAuth}); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("socks5 handshake: %w", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("socks5 handshake read: %w", err)
	}
	if resp[0] != socks5Version || resp[1] != socks5NoAuth {
		conn.Close()
		return nil, nil, fmt.Errorf("socks5 auth rejected: %v", resp)
	}

	// UDP ASSOCIATE request — client address 0.0.0.0:0
	req := []byte{
		socks5Version, socks5CmdUDP, 0x00,
		socks5AtypIPv4, 0, 0, 0, 0, 0, 0,
	}
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("socks5 udp associate write: %w", err)
	}

	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("socks5 udp associate read: %w", err)
	}
	if reply[1] != socks5Success {
		conn.Close()
		return nil, nil, fmt.Errorf("socks5 udp associate failed: 0x%02x", reply[1])
	}

	// Parse relay address from reply
	relayIP := net.IP(reply[4:8])
	relayPort := binary.BigEndian.Uint16(reply[8:10])

	// If relay IP is 0.0.0.0, use the proxy's IP
	if relayIP.Equal(net.IPv4zero) {
		host, _, _ := net.SplitHostPort(proxyAddr)
		relayIP = net.ParseIP(host)
	}

	return &net.UDPAddr{IP: relayIP, Port: int(relayPort)}, conn, nil
}
